#!/usr/bin/env python3
"""
评估脚本 v2 - 按题型判分
支持：事实抽取、定位类、总结类、对比类、不可回答类
"""
import json
import requests
import time
import re
from typing import Dict, List, Any, Tuple, Optional
from dataclasses import dataclass, field
from enum import Enum

# API 配置
API_URL = "http://localhost:8080/rag/query"
HEADERS = {"Content-Type": "application/json"}


class QuestionType(Enum):
    """问题类型"""
    FACT_EXTRACTION = "事实抽取"
    LOCATION = "定位类"
    SUMMARY = "总结类"
    COMPARISON = "对比类"
    UNANSWERABLE = "不可回答类"


@dataclass
class NumericValue:
    """数值结构"""
    value: float
    unit: str  # yuan, wan_yuan, yi_yuan, percent
    
    def to_yuan(self) -> float:
        """统一转换为元"""
        if self.unit == "yuan":
            return self.value
        elif self.unit == "wan_yuan":
            return self.value * 10000
        elif self.unit == "yi_yuan":
            return self.value * 100000000
        elif self.unit == "percent":
            return self.value  # 百分比保持原值
        return self.value
    
    def compare(self, other: 'NumericValue', tolerance: float = 0.01) -> bool:
        """比较两个数值，允许一定误差"""
        v1 = self.to_yuan()
        v2 = other.to_yuan()
        
        # 对于百分比，使用绝对误差
        if self.unit == "percent" or other.unit == "percent":
            return abs(v1 - v2) <= tolerance
        
        # 对于金额，使用相对误差
        if v2 == 0:
            return v1 == 0
        return abs(v1 - v2) / abs(v2) <= tolerance


def parse_numeric_value(text: str) -> Optional[NumericValue]:
    """从文本中提取数值和单位"""
    # 匹配数字（支持千分位逗号）
    number_pattern = r'[\d,]+\.?\d*'
    numbers = re.findall(number_pattern, text)
    
    if not numbers:
        return None
    
    # 取第一个数字
    num_str = numbers[0].replace(',', '')
    try:
        value = float(num_str)
    except ValueError:
        return None
    
    # 判断单位
    unit = "yuan"  # 默认元
    
    # 检查亿元
    if '亿元' in text or '亿' in text:
        unit = "yi_yuan"
    # 检查万元
    elif '万元' in text or '万' in text:
        unit = "wan_yuan"
    # 检查百分比
    elif '%' in text or 'percent' in text.lower() or '百分比' in text:
        unit = "percent"
        # 百分比值可能需要转换
        if value > 1 and '%' not in num_str:
            value = value / 100
    
    return NumericValue(value=value, unit=unit)


def normalize_section_title(title: str) -> List[str]:
    """章节标题标准化，返回别名列表"""
    title = title.strip().lower()
    
    # 章节别名映射
    aliases = {
        "公司业务概要": ["公司业务概要", "业务概要", "主营业务分析", "主营业务", "公司业务"],
        "管理层讨论与分析": ["管理层讨论与分析", "管理层讨论", "经营情况讨论", "经营分析"],
        "财务报表": ["财务报表", "财务报告", "会计报表", "财务数据"],
        "研发投入": ["研发投入", "研发支出", "研发费用", "研发情况"],
        "风险因素": ["风险因素", "风险提示", "风险分析", "重大风险"],
        "核心竞争力": ["核心竞争力", "竞争优势", "竞争分析", "市场地位"],
    }
    
    # 查找匹配的别名组
    for key, alias_list in aliases.items():
        if any(alias in title for alias in alias_list):
            return alias_list
    
    # 如果没有匹配，返回原标题
    return [title]


def match_section_title(predicted: str, gold: str) -> bool:
    """匹配章节标题，支持别名"""
    predicted_aliases = normalize_section_title(predicted)
    gold_aliases = normalize_section_title(gold)
    
    # 检查是否有交集
    return any(p in gold_aliases or g in predicted_aliases 
               for p in predicted_aliases for g in gold_aliases)


def extract_key_points(text: str) -> List[str]:
    """从文本中提取关键点"""
    # 按常见分隔符分割
    separators = ['；', '。', '\n', ';', ',', '，']
    points = [text]
    
    for sep in separators:
        new_points = []
        for point in points:
            new_points.extend([p.strip() for p in point.split(sep) if p.strip()])
        points = new_points
    
    # 过滤太短的点
    return [p for p in points if len(p) >= 5]


def check_unanswerable_hallucination(answer: str, citations: List[Dict]) -> Tuple[bool, str]:
    """检查不可回答类问题是否产生幻觉"""
    # 1. 检查是否编造具体数值
    numeric_values = re.findall(r'\d+\.?\d*\s*[万亿元%]', answer)
    if len(numeric_values) > 2:  # 如果有太多具体数值，可能是幻觉
        return False, "包含过多具体数值，可能是幻觉"
    
    # 2. 检查citations是否合理
    if citations:
        for citation in citations:
            title = citation.get("title", "").lower()
            # 如果citation标题明显与问题无关
            if any(keyword in title for keyword in ["错误", "test", "sample"]):
                return False, "citation来源不合理"
    
    # 3. 检查是否包含矛盾信息
    if "未提及" in answer and "为" in answer and "元" in answer:
        return False, "既说未提及又给出具体数值，矛盾"
    
    return True, "通过幻觉检查"


# ============ 按题型判分函数 ============

def judge_fact_extraction(item: Dict, answer: str, citations: List[Dict]) -> Tuple[bool, float, str, Dict]:
    """
    事实抽取题判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    gold_answer = item.get("gold_answer", "")
    
    # 使用结构化字段
    gold_value = item.get("gold_value")
    gold_unit = item.get("gold_unit")
    
    details = {
        "gold_value": gold_value,
        "gold_unit": gold_unit,
        "answer_numeric": None,
    }
    
    # 提取answer中的数值
    answer_numeric = parse_numeric_value(answer)
    details["answer_numeric"] = str(answer_numeric) if answer_numeric else None
    
    if gold_value is not None and gold_unit and answer_numeric:
        # 使用结构化的gold_value和gold_unit
        gold_numeric = NumericValue(value=gold_value, unit=gold_unit)
        
        # 数值比较
        if gold_numeric.compare(answer_numeric, tolerance=0.01):
            return True, 1.0, f"数值匹配: {gold_numeric.value} {gold_numeric.unit} ≈ {answer_numeric.value} {answer_numeric.unit}", details
        
        # 检查是否在可接受范围内（允许5%误差）
        if gold_numeric.compare(answer_numeric, tolerance=0.05):
            return True, 0.8, f"数值接近: {gold_numeric.value} {gold_numeric.unit} ≈ {answer_numeric.value} {answer_numeric.unit} (误差<5%)", details
        
        return False, 0.0, f"数值不匹配: gold={gold_numeric.value} {gold_numeric.unit}, answer={answer_numeric.value} {answer_numeric.unit}", details
    
    # 回退到旧逻辑
    gold_numeric = parse_numeric_value(gold_answer)
    if not gold_numeric or not answer_numeric:
        # 如果无法提取数值，退回到字符串匹配
        if gold_answer in answer or answer in gold_answer:
            return True, 1.0, "字符串匹配成功", details
        return False, 0.0, "无法提取数值进行匹配", details
    
    # 数值比较
    if gold_numeric.compare(answer_numeric, tolerance=0.01):
        return True, 1.0, f"数值匹配: {gold_numeric.value} {gold_numeric.unit} ≈ {answer_numeric.value} {answer_numeric.unit}", details
    
    # 检查是否在可接受范围内（允许5%误差）
    if gold_numeric.compare(answer_numeric, tolerance=0.05):
        return True, 0.8, f"数值接近: {gold_numeric.value} {gold_numeric.unit} ≈ {answer_numeric.value} {answer_numeric.unit} (误差<5%)", details
    
    return False, 0.0, f"数值不匹配: gold={gold_numeric.value} {gold_numeric.unit}, answer={answer_numeric.value} {answer_numeric.unit}", details


def judge_location(item: Dict, answer: str, citations: List[Dict]) -> Tuple[bool, float, str, Dict]:
    """
    定位类题判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    gold_section = item.get("gold_section_title", "")
    section_aliases = item.get("section_aliases", [])
    
    details = {
        "gold_section": gold_section,
        "section_aliases": section_aliases,
        "matched_in_answer": False,
        "matched_in_citation": False,
    }
    
    # 构建所有可能的章节名称（包括原始和别名）
    all_section_names = [gold_section]
    if section_aliases:
        all_section_names.extend(section_aliases)
    
    # 1. 检查answer中是否提到正确的章节
    for section_name in all_section_names:
        if section_name and match_section_title(answer, section_name):
            details["matched_in_answer"] = True
            return True, 1.0, f"答案中命中章节: {section_name}", details
    
    # 2. 检查citations中是否包含正确的章节
    if citations:
        for citation in citations:
            # 检查title
            title = citation.get("title", "")
            for section_name in all_section_names:
                if section_name and match_section_title(title, section_name):
                    details["matched_in_citation"] = True
                    return True, 0.9, f"引用中命中章节: {section_name}", details
            
            # 检查section_title字段（如果有）
            section_title = citation.get("section_title", "")
            for section_name in all_section_names:
                if section_title and section_name and match_section_title(section_title, section_name):
                    details["matched_in_citation"] = True
                    return True, 0.9, f"引用section_title命中: {section_name}", details
    
    return False, 0.0, f"未命中章节: {gold_section}", details


def judge_summary(item: Dict, answer: str, citations: List[Dict]) -> Tuple[bool, float, str, Dict]:
    """
    总结类题判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    gold_answer = item.get("gold_answer", "")
    gold_keypoints = item.get("gold_keypoints", [])
    
    # 使用结构化的gold_keypoints或从gold_answer提取
    gold_points = gold_keypoints if gold_keypoints else extract_key_points(gold_answer)
    answer_points = extract_key_points(answer)
    
    details = {
        "gold_keypoints": gold_keypoints,
        "gold_points_count": len(gold_points),
        "answer_points_count": len(answer_points),
        "matched_points": [],
        "coverage_rate": 0.0,
    }
    
    # 计算关键点覆盖率
    matched_count = 0
    for gold_point in gold_points:
        # 检查gold_point是否出现在answer中
        for answer_point in answer_points:
            if gold_point in answer_point or answer_point in gold_point:
                matched_count += 1
                details["matched_points"].append(gold_point)
                break
    
    coverage_rate = matched_count / len(gold_points) if gold_points else 0
    details["coverage_rate"] = coverage_rate
    
    # 根据覆盖率判分
    if coverage_rate >= 0.8:
        return True, 1.0, f"关键点覆盖率高: {coverage_rate:.0%}", details
    elif coverage_rate >= 0.5:
        return True, 0.7, f"关键点部分覆盖: {coverage_rate:.0%}", details
    elif coverage_rate >= 0.3:
        return False, 0.4, f"关键点覆盖不足: {coverage_rate:.0%}", details
    else:
        return False, 0.0, f"关键点覆盖过低: {coverage_rate:.0%}", details


def judge_comparison(item: Dict, answer: str, citations: List[Dict]) -> Tuple[bool, float, str, Dict]:
    """
    对比类题判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    # 使用结构化字段
    gold_base_value = item.get("gold_base_value")
    gold_compare_value = item.get("gold_compare_value")
    gold_delta_value = item.get("gold_delta_value")
    gold_delta_type = item.get("gold_delta_type")
    
    details = {
        "gold_base_value": gold_base_value,
        "gold_compare_value": gold_compare_value,
        "gold_delta_value": gold_delta_value,
        "gold_delta_type": gold_delta_type,
    }
    
    # 提取answer中的数值
    answer_numeric = parse_numeric_value(answer)
    details["answer_numeric"] = str(answer_numeric) if answer_numeric else None
    
    # 如果有结构化的delta值，直接比较
    if gold_delta_value is not None and answer_numeric:
        # 创建数值对象进行比较
        gold_numeric = NumericValue(value=gold_delta_value, unit="percent")
        
        # 数值比较
        if gold_numeric.compare(answer_numeric, tolerance=0.01):
            return True, 1.0, f"变化值匹配: {gold_delta_value}% ≈ {answer_numeric.value} {answer_numeric.unit}", details
        
        # 检查是否在可接受范围内（允许5%误差）
        if gold_numeric.compare(answer_numeric, tolerance=0.05):
            return True, 0.8, f"变化值接近: {gold_delta_value}% ≈ {answer_numeric.value} {answer_numeric.unit} (误差<5%)", details
        
        return False, 0.0, f"变化值不匹配: gold={gold_delta_value}%, answer={answer_numeric.value} {answer_numeric.unit}", details
    
    # 回退到事实抽取的逻辑
    return judge_fact_extraction(item, answer, citations)


def judge_unanswerable(item: Dict, answer: str, citations: List[Dict]) -> Tuple[bool, float, str, Dict]:
    """
    不可回答类题判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    details = {}
    
    # 1. 检查是否明确说明无法回答
    refusal_keywords = ["未提及", "未包含", "无法回答", "未找到", "不存在", "没有相关"]
    has_refusal = any(keyword in answer for keyword in refusal_keywords)
    
    if not has_refusal:
        # 如果没有明确拒绝，检查是否编造了答案
        pass_hallucination, reason = check_unanswerable_hallucination(answer, citations)
        if not pass_hallucination:
            return False, 0.0, f"未明确拒绝且{reason}", details
    
    # 2. 检查幻觉
    pass_hallucination, reason = check_unanswerable_hallucination(answer, citations)
    details["hallucination_check"] = reason
    
    if not pass_hallucination:
        return False, 0.0, f"产生幻觉: {reason}", details
    
    # 3. 检查citations是否合理
    if citations:
        details["citation_count"] = len(citations)
        # 不可回答的问题，citation应该很少或没有
        if len(citations) > 3:
            return True, 0.7, "正确识别为不可回答，但引用过多", details
    
    if has_refusal:
        return True, 1.0, "正确识别为不可回答并明确拒绝", details
    else:
        return True, 0.8, "正确识别为不可回答", details


# ============ 主判分函数 ============

def judge_by_type(item: Dict, response: Dict) -> Tuple[bool, float, str, Dict]:
    """
    根据题型进行判分
    返回: (是否正确, 分数, 原因, 详细结果)
    """
    question_type = item.get("type", "")
    answer = response.get("answer", "")
    citations = response.get("citations", [])
    
    # 根据题型选择判分函数
    if question_type == "事实抽取":
        return judge_fact_extraction(item, answer, citations)
    elif question_type == "定位类":
        return judge_location(item, answer, citations)
    elif question_type == "总结类":
        return judge_summary(item, answer, citations)
    elif question_type == "对比类":
        return judge_comparison(item, answer, citations)
    elif question_type == "不可回答类":
        return judge_unanswerable(item, answer, citations)
    else:
        # 默认使用事实抽取的逻辑
        return judge_fact_extraction(item, answer, citations)


# ============ 指标计算类 ============

@dataclass
class RetrievalMetrics:
    """检索指标"""
    gold_doc_hit_rate: float = 0.0
    gold_section_hit_rate: float = 0.0
    citation_precision: float = 0.0
    citation_recall: float = 0.0
    total: int = 0


@dataclass
class GenerationMetrics:
    """生成指标"""
    fact_exact_match: float = 0.0
    fact_numeric_match: float = 0.0
    summary_keypoint_coverage: float = 0.0
    refusal_correct_rate: float = 0.0
    total: int = 0


@dataclass
class EndToEndMetrics:
    """端到端指标"""
    task_success_rate: float = 0.0
    answerable_success_rate: float = 0.0
    unanswerable_success_rate: float = 0.0
    total: int = 0


@dataclass
class SystemMetrics:
    """系统指标"""
    timeout_rate: float = 0.0
    avg_latency: float = 0.0
    p95_latency: float = 0.0
    total: int = 0


class ComprehensiveMetrics:
    """综合指标"""
    def __init__(self):
        self.retrieval = RetrievalMetrics()
        self.generation = GenerationMetrics()
        self.end_to_end = EndToEndMetrics()
        self.system = SystemMetrics()
        
        # 按题型统计
        self.by_type: Dict[str, Dict] = {
            "事实抽取": {"correct": 0, "total": 0, "scores": []},
            "定位类": {"correct": 0, "total": 0, "scores": []},
            "总结类": {"correct": 0, "total": 0, "scores": []},
            "对比类": {"correct": 0, "total": 0, "scores": []},
            "不可回答类": {"correct": 0, "total": 0, "scores": []},
        }
        
        self.all_scores: List[float] = []
        self.latencies: List[float] = []
        self.timeout_count = 0
    
    def add_result(self, item: Dict, response: Dict, response_time: float, 
                   is_correct: bool, score: float, judge_reason: str, details: Dict):
        """添加评估结果"""
        question_type = item.get("type", "未知")
        answerable = item.get("answerable", True)
        
        # 系统指标
        self.system.total += 1
        self.latencies.append(response_time)
        if "error" in response and "timeout" in response.get("error", "").lower():
            self.timeout_count += 1
        
        # 端到端指标
        self.end_to_end.total += 1
        self.all_scores.append(score)
        
        if answerable:
            self.end_to_end.answerable_success_rate = (
                self.end_to_end.answerable_success_rate * (self.end_to_end.total - 1) + (1.0 if is_correct else 0.0)
            ) / self.end_to_end.total
        else:
            self.end_to_end.unanswerable_success_rate = (
                self.end_to_end.unanswerable_success_rate * (self.end_to_end.total - 1) + (1.0 if is_correct else 0.0)
            ) / self.end_to_end.total
        
        # 按题型统计
        if question_type in self.by_type:
            self.by_type[question_type]["total"] += 1
            self.by_type[question_type]["scores"].append(score)
            if is_correct:
                self.by_type[question_type]["correct"] += 1
        
        # 生成指标
        if question_type == "事实抽取":
            self.generation.total += 1
            if "numeric" in str(details):
                self.generation.fact_numeric_match = (
                    self.generation.fact_numeric_match * (self.generation.total - 1) + score
                ) / self.generation.total
        elif question_type == "总结类":
            self.generation.total += 1
            coverage = details.get("coverage_rate", 0)
            self.generation.summary_keypoint_coverage = (
                self.generation.summary_keypoint_coverage * (self.generation.total - 1) + coverage
            ) / self.generation.total
        elif question_type == "不可回答类":
            self.generation.total += 1
            if is_correct:
                self.generation.refusal_correct_rate = (
                    self.generation.refusal_correct_rate * (self.generation.total - 1) + 1.0
                ) / self.generation.total
    
    def finalize(self):
        """计算最终指标"""
        # 系统指标
        if self.latencies:
            self.system.avg_latency = sum(self.latencies) / len(self.latencies)
            self.system.p95_latency = sorted(self.latencies)[int(len(self.latencies) * 0.95)] if len(self.latencies) > 1 else self.latencies[0]
            self.system.timeout_rate = self.timeout_count / self.system.total if self.system.total > 0 else 0
        
        # 端到端指标
        if self.all_scores:
            self.end_to_end.task_success_rate = sum(1 for s in self.all_scores if s >= 0.7) / len(self.all_scores)
        
        # 按题型计算准确率
        for qtype, stats in self.by_type.items():
            if stats["total"] > 0:
                stats["accuracy"] = stats["correct"] / stats["total"]
                stats["avg_score"] = sum(stats["scores"]) / len(stats["scores"])
    
    def get_report(self) -> Dict:
        """生成评估报告"""
        self.finalize()
        return {
            "retrieval_metrics": {
                "gold_doc_hit_rate": self.retrieval.gold_doc_hit_rate,
                "gold_section_hit_rate": self.retrieval.gold_section_hit_rate,
                "citation_precision": self.retrieval.citation_precision,
            },
            "generation_metrics": {
                "fact_numeric_match": self.generation.fact_numeric_match,
                "summary_keypoint_coverage": self.generation.summary_keypoint_coverage,
                "refusal_correct_rate": self.generation.refusal_correct_rate,
            },
            "end_to_end_metrics": {
                "task_success_rate": self.end_to_end.task_success_rate,
                "answerable_success_rate": self.end_to_end.answerable_success_rate,
                "unanswerable_success_rate": self.end_to_end.unanswerable_success_rate,
            },
            "system_metrics": {
                "timeout_rate": self.system.timeout_rate,
                "avg_latency_ms": self.system.avg_latency * 1000,
                "p95_latency_ms": self.system.p95_latency * 1000,
            },
            "by_type": self.by_type,
        }


# ============ 加载和调用函数 ============

def load_dataset(file_path: str) -> List[Dict[str, Any]]:
    """加载评测集"""
    with open(file_path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return data.get("dataset", [])


def call_api(item: Dict[str, Any]) -> Tuple[Dict[str, Any], float]:
    """调用 API"""
    payload = {
        "question": item.get("question"),
        "stock_code": item.get("stock_code"),
        "time_range": item.get("time_range"),
        "doc_types": item.get("doc_types"),
        "top_k": 4
    }
    
    start_time = time.time()
    try:
        response = requests.post(API_URL, headers=HEADERS, json=payload, timeout=60)
        response_time = time.time() - start_time
        
        if response.status_code == 200:
            return response.json(), response_time
        else:
            return {"error": f"API error: {response.status_code}"}, response_time
    except Exception as e:
        response_time = time.time() - start_time
        return {"error": f"Request error: {str(e)}"}, response_time


def evaluate(dataset: List[Dict[str, Any]]):
    """评估主函数"""
    metrics = ComprehensiveMetrics()
    results = []
    
    for item in dataset:
        print(f"Evaluating question {item.get('id')}: {item.get('question')}")
        
        response, response_time = call_api(item)
        
        # 按题型判分
        is_correct, score, judge_reason, details = judge_by_type(item, response)
        
        metrics.add_result(item, response, response_time, is_correct, score, judge_reason, details)
        
        # 合并重复的citations
        citations = response.get("citations", [])
        citation_map = {}
        for citation in citations:
            if "title" in citation:
                citation_map[citation["title"]] = citation
        unique_citations = list(citation_map.values())
        
        # 保存详细结果
        result = {
            "id": item.get("id"),
            "question": item.get("question"),
            "stock_code": item.get("stock_code"),
            "type": item.get("type"),
            "answerable": item.get("answerable"),
            "gold_answer": item.get("gold_answer"),
            "gold_section_title": item.get("gold_section_title"),
            "predicted_answer": response.get("answer"),
            "citations": unique_citations,
            "response_time": response_time,
            "error": response.get("error"),
            # 新增字段
            "is_correct": is_correct,
            "score": score,
            "judge_reason": judge_reason,
            "judge_details": details,
            "retrieval_hit_doc": None,  # 待实现
            "retrieval_hit_section": details.get("matched_in_citation") if item.get("type") == "定位类" else None,
            "answer_correct": is_correct,
            "citation_correct": None,  # 待实现
        }
        results.append(result)
        
        # 避免请求过快
        time.sleep(0.5)
    
    # 生成报告
    report = metrics.get_report()
    
    # 保存评估结果
    with open("eval_results_v2.json", "w", encoding="utf-8") as f:
        json.dump({
            "metrics": report,
            "results": results
        }, f, ensure_ascii=False, indent=2)
    
    # 打印评估报告
    print("\n" + "="*60)
    print("评估报告 v2")
    print("="*60)
    
    print("\n【检索指标】")
    for key, value in report["retrieval_metrics"].items():
        print(f"  {key}: {value:.4f}")
    
    print("\n【生成指标】")
    for key, value in report["generation_metrics"].items():
        print(f"  {key}: {value:.4f}")
    
    print("\n【端到端指标】")
    for key, value in report["end_to_end_metrics"].items():
        print(f"  {key}: {value:.4f}")
    
    print("\n【系统指标】")
    for key, value in report["system_metrics"].items():
        if "latency" in key:
            print(f"  {key}: {value:.2f}ms")
        else:
            print(f"  {key}: {value:.4f}")
    
    print("\n【按题型统计】")
    for qtype, stats in report["by_type"].items():
        if stats["total"] > 0:
            print(f"  {qtype}:")
            print(f"    准确率: {stats.get('accuracy', 0):.2%}")
            print(f"    平均分: {stats.get('avg_score', 0):.2f}")
            print(f"    正确/总数: {stats['correct']}/{stats['total']}")


if __name__ == "__main__":
    dataset = load_dataset("eval_dataset_v2.json")
    evaluate(dataset)
