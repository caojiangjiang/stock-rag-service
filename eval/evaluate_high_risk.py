#!/usr/bin/env python3
import argparse
import json
import re
import time
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Dict, List, Tuple

import requests


DEFAULT_API_URL = "http://localhost:8080/rag/query"
DEFAULT_HEADERS = {"Content-Type": "application/json"}
DEFAULT_DATASET = Path(__file__).with_name("high_risk_eval_dataset.json")
DEFAULT_OUTPUT = Path(__file__).with_name("high_risk_eval_results.json")
REFUSAL_HINTS = ["证据不足", "无法回答", "无法直接回答", "未检索到", "未披露", "没有可", "不能", "未找到"]
NUMBER_RE = re.compile(r"\d[\d,]*(?:\.\d+)?")
MONEY_UNIT_RE = re.compile(r"\d[\d,]*(?:\.\d+)?\s*(?:元|万元|亿元|%)")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Evaluate high-risk financial RAG cases.")
    parser.add_argument("--dataset", default=str(DEFAULT_DATASET), help="Path to high risk dataset JSON")
    parser.add_argument("--output", default=str(DEFAULT_OUTPUT), help="Path to evaluation result JSON")
    parser.add_argument("--api-url", default=DEFAULT_API_URL, help="RAG query endpoint")
    parser.add_argument("--top-k", type=int, default=5, help="top_k passed to query API")
    parser.add_argument("--timeout", type=int, default=60, help="request timeout in seconds")
    parser.add_argument("--sleep", type=float, default=0.2, help="sleep seconds between requests")
    parser.add_argument("--limit", type=int, default=0, help="only evaluate first N cases")
    parser.add_argument("--dry-run", action="store_true", help="only validate dataset and print summary")
    parser.add_argument("--local-only", dest="local_only", action="store_true", default=True, help="force query API to use only testdata/local_documents.json")
    parser.add_argument("--no-local-only", dest="local_only", action="store_false", help="allow query API to use mixed runtime data sources")
    return parser.parse_args()


def load_dataset(path: str) -> Dict[str, Any]:
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def normalize_text(text: str) -> str:
    return re.sub(r"\s+", "", (text or "").lower())


def contains_keyword(text: str, keyword: str) -> bool:
    return normalize_text(keyword) in normalize_text(text)


def contains_any(text: str, keywords: List[str]) -> bool:
    return any(contains_keyword(text, kw) for kw in keywords)


def keyword_coverage(text: str, keywords: List[str]) -> Tuple[int, int]:
    if not keywords:
        return 0, 0
    hit = sum(1 for kw in keywords if contains_keyword(text, kw))
    return hit, len(keywords)


def looks_like_refusal(answer: str) -> bool:
    return contains_any(answer, REFUSAL_HINTS)


def citation_titles(response: Dict[str, Any]) -> List[str]:
    return [c.get("title", "") for c in response.get("citations", []) if c.get("title")]


def has_allowed_citation(response: Dict[str, Any], allowed_titles: List[str]) -> bool:
    if not allowed_titles:
        return True
    titles = citation_titles(response)
    for actual in titles:
        for allowed in allowed_titles:
            if contains_keyword(actual, allowed) or contains_keyword(allowed, actual):
                return True
    return False


def sentence_has_year_claim(sentence: str, year: int) -> bool:
    if not year:
        return False
    if f"{year}年" not in sentence:
        return False
    return bool(MONEY_UNIT_RE.search(sentence) or (NUMBER_RE.search(sentence) and any(tok in sentence for tok in ["为", "达到", "实现", "录得", "是"])))


def split_sentences(text: str) -> List[str]:
    return [seg.strip() for seg in re.split(r"[。；\n]", text or "") if seg.strip()]


def has_forbidden_year_claim(answer: str, years: List[int]) -> bool:
    for sentence in split_sentences(answer):
        for year in years:
            if sentence_has_year_claim(sentence, year):
                return True
    return False


def has_target_year_value_claim(answer: str, target_year: int) -> bool:
    return has_forbidden_year_claim(answer, [target_year])


def value_matches(answer: str, expected_value: str) -> bool:
    if not expected_value:
        return True
    if contains_keyword(answer, expected_value):
        return True
    expected_numbers = NUMBER_RE.findall(expected_value)
    answer_numbers = NUMBER_RE.findall(answer)
    return bool(expected_numbers) and any(num in answer_numbers for num in expected_numbers)


def build_payload(item: Dict[str, Any], top_k: int, local_only: bool) -> Dict[str, Any]:
    return {
        "question": item.get("question"),
        "stock_code": item.get("stock_code"),
        "time_range": item.get("time_range"),
        "doc_types": item.get("doc_types"),
        "top_k": top_k,
        "use_local_only": local_only,
    }


def call_api(session: requests.Session, api_url: str, item: Dict[str, Any], top_k: int, timeout: int, local_only: bool) -> Tuple[Dict[str, Any], float]:
    start = time.time()
    try:
        resp = session.post(api_url, headers=DEFAULT_HEADERS, json=build_payload(item, top_k, local_only), timeout=timeout)
        elapsed = time.time() - start
        if resp.status_code != 200:
            return {"error": f"HTTP {resp.status_code}", "body": resp.text}, elapsed
        return resp.json(), elapsed
    except Exception as exc:
        return {"error": str(exc)}, time.time() - start


def evaluate_answer_case(item: Dict[str, Any], response: Dict[str, Any]) -> Tuple[bool, float, List[str], Dict[str, Any]]:
    answer = response.get("answer", "")
    reasons, detail = [], {}
    score = 0.0

    if not answer.strip():
        return False, 0.0, ["empty_answer"], detail
    if looks_like_refusal(answer):
        reasons.append("unexpected_refusal")
    else:
        score += 0.25

    if value_matches(answer, item.get("expected_value")):
        score += 0.35
        detail["value_match"] = True
    else:
        detail["value_match"] = False
        if item.get("expected_value"):
            reasons.append("missing_expected_value")

    hit, total = keyword_coverage(answer, item.get("expected_keywords", []))
    detail["keyword_hit"] = {"hit": hit, "total": total}
    if total == 0 or hit / total >= 0.5:
        score += 0.20
    else:
        reasons.append("low_keyword_coverage")

    if has_allowed_citation(response, item.get("allowed_evidence_titles", [])):
        score += 0.20
        detail["citation_match"] = True
    else:
        reasons.append("citation_mismatch")
        detail["citation_match"] = False

    forbidden_years = item.get("must_not_include_years", [])
    if has_forbidden_year_claim(answer, forbidden_years):
        reasons.append("forbidden_year_claim")
        score = min(score, 0.2)

    passed = score >= 0.7 and "unexpected_refusal" not in reasons and "forbidden_year_claim" not in reasons
    return passed, round(score, 4), reasons, detail


def evaluate_refuse_case(item: Dict[str, Any], response: Dict[str, Any]) -> Tuple[bool, float, List[str], Dict[str, Any]]:
    answer = response.get("answer", "")
    reasons, detail = [], {}
    score = 0.0
    target_year = item.get("expected_year")

    if looks_like_refusal(answer):
        score += 0.5
        detail["refusal_detected"] = True
    else:
        reasons.append("missing_refusal_signal")
        detail["refusal_detected"] = False

    if not has_forbidden_year_claim(answer, item.get("must_not_include_years", [])):
        score += 0.3
    else:
        reasons.append("forbidden_year_claim")

    if target_year and not has_target_year_value_claim(answer, target_year):
        score += 0.2
    elif target_year:
        reasons.append("fabricated_target_year_value")

    hit, total = keyword_coverage(answer, item.get("expected_keywords", []))
    detail["keyword_hit"] = {"hit": hit, "total": total}
    if total and hit == 0:
        reasons.append("missing_refusal_keywords")

    passed = score >= 0.8 and not any(r in reasons for r in ["forbidden_year_claim", "fabricated_target_year_value"])
    return passed, round(score, 4), reasons, detail


def evaluate_item(item: Dict[str, Any], response: Dict[str, Any]) -> Tuple[bool, float, List[str], Dict[str, Any]]:
    if "error" in response:
        return False, 0.0, ["api_error"], {"error": response.get("error")}
    behavior = item.get("expected_behavior", "ANSWER").upper()
    if behavior == "REFUSE":
        return evaluate_refuse_case(item, response)
    return evaluate_answer_case(item, response)


def summarize(results: List[Dict[str, Any]]) -> Dict[str, Any]:
    total = len(results)
    passed = sum(1 for r in results if r["passed"])
    avg_latency = sum(r["response_time"] for r in results) / total if total else 0.0
    by_behavior, by_dimension, reason_counter = defaultdict(lambda: {"total": 0, "passed": 0}), defaultdict(lambda: {"total": 0, "passed": 0}), Counter()

    for result in results:
        for bucket, key in [(by_behavior, result["expected_behavior"]), (by_dimension, result["eval_dimension"] )]:
            bucket[key]["total"] += 1
            bucket[key]["passed"] += int(result["passed"])
        for reason in result["reasons"]:
            reason_counter[reason] += 1

    return {
        "total": total,
        "passed": passed,
        "failed": total - passed,
        "pass_rate": round(passed / total, 4) if total else 0.0,
        "avg_response_time": round(avg_latency, 4),
        "by_behavior": by_behavior,
        "by_dimension": by_dimension,
        "top_failure_reasons": reason_counter.most_common(10),
    }


def main() -> None:
    args = parse_args()
    dataset_obj = load_dataset(args.dataset)
    dataset = dataset_obj.get("dataset", [])
    if args.limit > 0:
        dataset = dataset[:args.limit]

    if args.dry_run:
        summary = Counter(item.get("expected_behavior", "UNKNOWN") for item in dataset)
        print(f"dataset={args.dataset}")
        print(f"cases={len(dataset)}")
        print(f"behavior_distribution={dict(summary)}")
        return

    session = requests.Session()
    results: List[Dict[str, Any]] = []

    for idx, item in enumerate(dataset, start=1):
        print(f"[{idx}/{len(dataset)}] {item['id']} -> {item['question']}")
        response, response_time = call_api(session, args.api_url, item, args.top_k, args.timeout, args.local_only)
        passed, score, reasons, detail = evaluate_item(item, response)
        results.append({
            "id": item.get("id"),
            "question": item.get("question"),
            "expected_behavior": item.get("expected_behavior"),
            "eval_dimension": item.get("eval_dimension"),
            "passed": passed,
            "score": score,
            "reasons": reasons,
            "detail": detail,
            "response_time": round(response_time, 4),
            "response": response,
        })
        time.sleep(args.sleep)

    output = {
        "meta": {
            "dataset": str(args.dataset),
            "api_url": args.api_url,
            "top_k": args.top_k,
            "local_only": args.local_only,
            "evaluated_at": int(time.time()),
        },
        "summary": summarize(results),
        "results": results,
    }
    with open(args.output, "w", encoding="utf-8") as f:
        json.dump(output, f, ensure_ascii=False, indent=2)

    print("\nSummary:")
    print(json.dumps(output["summary"], ensure_ascii=False, indent=2))
    print(f"\nSaved to: {args.output}")


if __name__ == "__main__":
    main()