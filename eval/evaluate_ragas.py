#!/usr/bin/env python3
import json
import requests
import time
from typing import Dict, List, Any
from ragas import evaluate
from ragas.metrics.collections import (
    Faithfulness,        # 忠实度
    AnswerRelevancy,     # 答案相关性
    ContextRelevance     # 上下文相关性（替代RetrievalRelevance）
)

# API 配置
API_URL = "http://localhost:8080/rag/query"
HEADERS = {"Content-Type": "application/json"}

# 加载评测集
def load_dataset(file_path: str) -> List[Dict[str, Any]]:
    with open(file_path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return data.get("dataset", [])

# 调用 API
def call_api(item: Dict[str, Any]) -> Dict[str, Any]:
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

# 准备 RAGAs 评估数据
def prepare_ragas_data(dataset: List[Dict[str, Any]]):
    questions = []
    answers = []
    contexts = []
    ground_truths = []
    metadata = []
    
    for item in dataset:
        print(f"Preparing data for question {item.get('id')}: {item.get('question')}")
        
        response, response_time = call_api(item)
        
        # 检查是否有错误
        if "error" in response:
            print(f"Error for question {item.get('id')}: {response['error']}")
            continue
        
        # 提取数据
        question = item.get("question")
        answer = response.get("answer", "")
        citations = response.get("citations", [])
        ground_truth = item.get("gold_answer", "")
        
        # 从 citations 中提取上下文
        context_list = []
        for citation in citations:
            # 提取 content 或 title 作为上下文
            if "content" in citation:
                context_list.append(citation["content"])
            elif "title" in citation:
                context_list.append(citation["title"])
        
        # 只添加有上下文的样本
        if context_list:
            questions.append(question)
            answers.append(answer)
            contexts.append(context_list)
            ground_truths.append([ground_truth])  # RAGAs 期望 ground_truth 是列表
            metadata.append({
                "id": item.get("id"),
                "stock_code": item.get("stock_code"),
                "type": item.get("type"),
                "answerable": item.get("answerable"),
                "response_time": response_time
            })
        
        # 避免请求过快
        time.sleep(0.5)
    
    return questions, answers, contexts, ground_truths, metadata

# 评估
def evaluate_with_ragas():
    # 加载数据集
    dataset = load_dataset("eval_dataset.json")
    
    # 准备 RAGAs 数据
    questions, answers, contexts, ground_truths, metadata = prepare_ragas_data(dataset)
    
    # 检查数据是否足够
    if len(questions) == 0:
        print("No valid data for evaluation")
        return
    
    print(f"Prepared {len(questions)} samples for evaluation")
    
    # 执行评估 - 不指定LLM，使用默认值
    print("Running RAGAs evaluation...")
    result = evaluate(
        dataset={
            "question": questions,
            "answer": answers,
            "contexts": contexts,
            "ground_truth": ground_truths
        }
    )
    
    # 打印结果
    print("\nRAGAs Evaluation Results:")
    print(result)
    
    # 保存详细结果
    detailed_results = {
        "overall_metrics": result.to_dict(),
        "detailed_samples": []
    }
    
    # 为每个样本保存详细信息
    for i in range(len(questions)):
        sample_result = {
            "id": metadata[i].get("id"),
            "question": questions[i],
            "answer": answers[i],
            "contexts": contexts[i],
            "ground_truth": ground_truths[i],
            "metadata": metadata[i]
        }
        detailed_results["detailed_samples"].append(sample_result)
    
    # 保存评估结果
    with open("eval_results_ragas.json", "w", encoding="utf-8") as f:
        json.dump(detailed_results, f, ensure_ascii=False, indent=2)
    
    print("\nEvaluation results saved to eval_results_ragas.json")

if __name__ == "__main__":
    evaluate_with_ragas()
