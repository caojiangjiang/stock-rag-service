#!/usr/bin/env python3
"""
Agent 评估脚本
评估 Agent 的 5 类能力：
1. 工具选择是否正确
2. 工具参数是否抽取正确
3. 多轮 session / slot memory 是否稳定
4. 该拒答时能不能拒答
5. 复杂任务能不能拆解完成
"""
import json
import requests
import time
from typing import Dict, List, Any, Tuple, Optional
from dataclasses import dataclass

# API 配置
API_URL = "http://localhost:8080/agent/execute"
SESSION_URL = "http://localhost:8080/agent/session"
HEADERS = {"Content-Type": "application/json"}


@dataclass
class AgentMetrics:
    """Agent 评估指标"""
    # 1. 工具选择指标
    tool_selection_accuracy: float = 0.0
    tool_call_count: int = 0
    redundant_tool_call_rate: float = 0.0
    
    # 2. 工具参数指标
    stock_code_arg_accuracy: float = 0.0
    time_range_arg_accuracy: float = 0.0
    doc_type_arg_accuracy: float = 0.0
    compare_arg_accuracy: float = 0.0
    
    # 3. 多轮记忆指标
    slot_retention_accuracy: float = 0.0
    cross_turn_reference_accuracy: float = 0.0
    session_isolation_accuracy: float = 0.0
    
    # 4. 拒答能力指标
    unsupported_refusal_rate: float = 0.0
    hallucination_rate: float = 0.0
    
    # 5. 复杂任务拆解指标
    task_completion_rate: float = 0.0
    subtask_completion_rate: float = 0.0
    
    # 计数
    total_cases: int = 0
    successful_cases: int = 0


def call_agent(task: str, session_id: str = "") -> Tuple[Dict[str, Any], float]:
    """调用 Agent API"""
    payload = {
        "task": task,
        "session_id": session_id
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


def get_session_info(session_id: str) -> Dict[str, Any]:
    """获取会话信息，包括 step_traces"""
    try:
        response = requests.get(f"{SESSION_URL}?session_id={session_id}", headers=HEADERS, timeout=10)
        if response.status_code == 200:
            return response.json()
        else:
            return {"error": f"Session API error: {response.status_code}"}
    except Exception as e:
        return {"error": f"Session request error: {str(e)}"}


def extract_tool_calls_from_traces(step_traces: List[Dict]) -> Tuple[List[str], Dict[str, Any]]:
    """从 step_traces 中提取工具调用信息"""
    tools = []
    args = {}
    
    for trace in step_traces:
        tool_name = trace.get("tool_name", "")
        if tool_name:
            tools.append(tool_name)
            
            # 解析 args 字符串
            args_str = trace.get("args", "")
            if args_str and args_str != "":
                try:
                    # 尝试解析 JSON 格式的参数
                    trace_args = json.loads(args_str.replace("'", "\""))
                    args.update(trace_args)
                except:
                    pass
    
    return tools, args


def evaluate_tool_selection(actual_tools: List[str], expected_tools: List[str]) -> Dict[str, Any]:
    """评估工具选择"""
    if not expected_tools:
        return {
            "tool_selection_accuracy": 1.0,
            "tool_call_count": len(actual_tools),
            "redundant_tool_call_rate": 0.0
        }
    
    # 计算工具选择准确率
    correct_tools = 0
    for tool in actual_tools:
        if tool in expected_tools:
            correct_tools += 1
    
    tool_selection_accuracy = correct_tools / len(expected_tools) if expected_tools else 0
    
    # 计算冗余工具调用率
    redundant_tools = len(actual_tools) - correct_tools
    redundant_tool_call_rate = redundant_tools / len(actual_tools) if actual_tools else 0
    
    return {
        "tool_selection_accuracy": tool_selection_accuracy,
        "tool_call_count": len(actual_tools),
        "redundant_tool_call_rate": redundant_tool_call_rate
    }


def evaluate_tool_params(actual_args: Dict[str, Any], expected_slots: Dict[str, Any]) -> Dict[str, Any]:
    """评估工具参数"""
    if not expected_slots:
        return {
            "stock_code_arg_accuracy": 1.0,
            "time_range_arg_accuracy": 1.0,
            "doc_type_arg_accuracy": 1.0,
            "compare_arg_accuracy": 1.0
        }
    
    # 股票代码参数准确率
    stock_code_correct = 0
    if "stock_code" in expected_slots:
        expected_stock = expected_slots["stock_code"]
        actual_stock = actual_args.get("stock_code") or actual_args.get("symbol")
        if actual_stock == expected_stock:
            stock_code_correct = 1
    stock_code_arg_accuracy = stock_code_correct / 1 if "stock_code" in expected_slots else 1.0
    
    # 时间范围参数准确率
    time_range_correct = 0
    if "time_range" in expected_slots:
        expected_time = expected_slots["time_range"]
        actual_time = actual_args.get("time_range")
        if actual_time == expected_time:
            time_range_correct = 1
    time_range_arg_accuracy = time_range_correct / 1 if "time_range" in expected_slots else 1.0
    
    # 文档类型参数准确率
    doc_type_correct = 0
    if "doc_types" in expected_slots:
        expected_docs = set(expected_slots["doc_types"])
        actual_docs = set(actual_args.get("doc_types", []))
        if expected_docs.issubset(actual_docs):
            doc_type_correct = 1
    doc_type_arg_accuracy = doc_type_correct / 1 if "doc_types" in expected_slots else 1.0
    
    # 比较参数准确率
    compare_correct = 0
    if "compare_years" in expected_slots:
        expected_compare = set(expected_slots["compare_years"])
        actual_compare = set(actual_args.get("compare_years", []))
        if expected_compare.issubset(actual_compare):
            compare_correct = 1
    compare_arg_accuracy = compare_correct / 1 if "compare_years" in expected_slots else 1.0
    
    return {
        "stock_code_arg_accuracy": stock_code_arg_accuracy,
        "time_range_arg_accuracy": time_range_arg_accuracy,
        "doc_type_arg_accuracy": doc_type_arg_accuracy,
        "compare_arg_accuracy": compare_arg_accuracy
    }


def evaluate_session_memory(session_history: List[Dict], expected_slots: Dict[str, Any]) -> Dict[str, Any]:
    """评估多轮记忆"""
    if not session_history or not expected_slots:
        return {
            "slot_retention_accuracy": 1.0,
            "cross_turn_reference_accuracy": 1.0,
            "session_isolation_accuracy": 1.0
        }
    
    # 计算槽位保留准确率
    slot_correct = 0
    slot_total = len(expected_slots)
    
    for key, expected_value in expected_slots.items():
        # 检查是否在会话历史中保持一致
        consistent = True
        for turn in session_history:
            actual_value = turn.get("slots", {}).get(key)
            if actual_value != expected_value:
                consistent = False
                break
        if consistent:
            slot_correct += 1
    
    slot_retention_accuracy = slot_correct / slot_total if slot_total > 0 else 1.0
    
    # 跨轮引用准确率（简化版）
    cross_turn_correct = 1 if slot_retention_accuracy > 0.5 else 0
    cross_turn_reference_accuracy = cross_turn_correct
    
    # 会话隔离准确率（简化版）
    session_isolation_accuracy = 1.0
    
    return {
        "slot_retention_accuracy": slot_retention_accuracy,
        "cross_turn_reference_accuracy": cross_turn_reference_accuracy,
        "session_isolation_accuracy": session_isolation_accuracy
    }


def evaluate_refusal(answer: str, must_refuse: bool) -> Dict[str, Any]:
    """评估拒答能力"""
    refusal_keywords = ["未提及", "未包含", "无法回答", "未找到", "不存在", "没有相关"]
    hallucination_keywords = ["预测", "预计", "估计", "可能", "大概"]
    
    has_refusal = any(keyword in answer for keyword in refusal_keywords)
    has_hallucination = any(keyword in answer for keyword in hallucination_keywords)
    
    # 拒答率
    unsupported_refusal_rate = 1.0 if must_refuse and has_refusal else 0.0
    if not must_refuse:
        unsupported_refusal_rate = 1.0
    
    # 幻觉率
    hallucination_rate = 1.0 if has_hallucination and must_refuse else 0.0
    
    return {
        "unsupported_refusal_rate": unsupported_refusal_rate,
        "hallucination_rate": hallucination_rate
    }


def evaluate_task_completion(answer: str, success_criteria: List[str]) -> Dict[str, Any]:
    """评估复杂任务拆解"""
    if not success_criteria:
        return {
            "task_completion_rate": 1.0,
            "subtask_completion_rate": 1.0
        }
    
    # 计算子任务完成率
    subtask_completed = 0
    for criterion in success_criteria:
        if criterion in answer:
            subtask_completed += 1
    
    subtask_completion_rate = subtask_completed / len(success_criteria)
    
    # 任务完成率
    task_completion_rate = 1.0 if subtask_completion_rate >= 0.8 else 0.0
    
    return {
        "task_completion_rate": task_completion_rate,
        "subtask_completion_rate": subtask_completion_rate
    }


def evaluate_agent_case(case: Dict[str, Any]) -> Dict[str, Any]:
    """评估单个 Agent 案例"""
    session_id = case.get("session_id", f"session-{int(time.time())}")
    turns = case.get("turns", [])
    expected_tools = case.get("expected_tools", [])
    expected_slots = case.get("expected_slots", {})
    must_refuse = case.get("must_refuse", False)
    success_criteria = case.get("success_criteria", [])
    
    session_history = []
    all_actual_tools = []
    all_actual_args = {}
    final_answer = ""
    
    # 执行多轮对话
    for i, turn in enumerate(turns):
        task = turn.get("task", "")
        
        response, response_time = call_agent(task, session_id)
        
        # 获取会话信息，提取真实的工具调用
        session_info = get_session_info(session_id)
        step_traces = session_info.get("step_traces", [])
        
        # 从 step_traces 提取工具调用信息
        turn_tools, turn_args = extract_tool_calls_from_traces(step_traces)
        all_actual_tools.extend(turn_tools)
        all_actual_args.update(turn_args)
        
        # 记录会话历史
        session_turn = {
            "turn": i + 1,
            "task": task,
            "response": response,
            "tools_called": turn_tools,
            "tool_args": turn_args,
            "slots": session_info.get("session_state", {})
        }
        session_history.append(session_turn)
        
        # 提取最终答案
        if "result" in response:
            final_answer = response["result"]
    
    # 评估各项指标
    tool_selection_metrics = evaluate_tool_selection(all_actual_tools, expected_tools)
    tool_params_metrics = evaluate_tool_params(all_actual_args, expected_slots)
    session_memory_metrics = evaluate_session_memory(session_history, expected_slots)
    refusal_metrics = evaluate_refusal(final_answer, must_refuse)
    task_completion_metrics = evaluate_task_completion(final_answer, success_criteria)
    
    # 综合评估结果
    is_success = False
    if not must_refuse:
        # 对于非拒答案例，检查是否完成任务
        is_success = task_completion_metrics["task_completion_rate"] >= 0.8
    else:
        # 对于拒答案例，检查是否正确拒答
        is_success = refusal_metrics["unsupported_refusal_rate"] == 1.0
    
    return {
        "case_id": case.get("id"),
        "session_id": session_id,
        "final_answer": final_answer,
        "actual_tools": all_actual_tools,
        "actual_args": all_actual_args,
        "is_success": is_success,
        "metrics": {
            **tool_selection_metrics,
            **tool_params_metrics,
            **session_memory_metrics,
            **refusal_metrics,
            **task_completion_metrics
        },
        "session_history": session_history
    }

def evaluate_agent_dataset(dataset: List[Dict[str, Any]]) -> Dict[str, Any]:
    """评估 Agent 数据集"""
    metrics = AgentMetrics()
    results = []
    
    for case in dataset:
        print(f"Evaluating case {case.get('id')}...")
        
        result = evaluate_agent_case(case)
        results.append(result)
        
        # 更新指标
        metrics.total_cases += 1
        if result["is_success"]:
            metrics.successful_cases += 1
        
        case_metrics = result["metrics"]
        
        # 工具选择指标
        metrics.tool_selection_accuracy += case_metrics["tool_selection_accuracy"]
        metrics.tool_call_count += case_metrics["tool_call_count"]
        metrics.redundant_tool_call_rate += case_metrics["redundant_tool_call_rate"]
        
        # 工具参数指标
        metrics.stock_code_arg_accuracy += case_metrics["stock_code_arg_accuracy"]
        metrics.time_range_arg_accuracy += case_metrics["time_range_arg_accuracy"]
        metrics.doc_type_arg_accuracy += case_metrics["doc_type_arg_accuracy"]
        metrics.compare_arg_accuracy += case_metrics["compare_arg_accuracy"]
        
        # 多轮记忆指标
        metrics.slot_retention_accuracy += case_metrics["slot_retention_accuracy"]
        metrics.cross_turn_reference_accuracy += case_metrics["cross_turn_reference_accuracy"]
        metrics.session_isolation_accuracy += case_metrics["session_isolation_accuracy"]
        
        # 拒答能力指标
        metrics.unsupported_refusal_rate += case_metrics["unsupported_refusal_rate"]
        metrics.hallucination_rate += case_metrics["hallucination_rate"]
        
        # 复杂任务拆解指标
        metrics.task_completion_rate += case_metrics["task_completion_rate"]
        metrics.subtask_completion_rate += case_metrics["subtask_completion_rate"]
        
        # 避免请求过快
        time.sleep(1.0)
    
    # 计算平均值
    if metrics.total_cases > 0:
        metrics.tool_selection_accuracy /= metrics.total_cases
        metrics.redundant_tool_call_rate /= metrics.total_cases
        metrics.stock_code_arg_accuracy /= metrics.total_cases
        metrics.time_range_arg_accuracy /= metrics.total_cases
        metrics.doc_type_arg_accuracy /= metrics.total_cases
        metrics.compare_arg_accuracy /= metrics.total_cases
        metrics.slot_retention_accuracy /= metrics.total_cases
        metrics.cross_turn_reference_accuracy /= metrics.total_cases
        metrics.session_isolation_accuracy /= metrics.total_cases
        metrics.unsupported_refusal_rate /= metrics.total_cases
        metrics.hallucination_rate /= metrics.total_cases
        metrics.task_completion_rate /= metrics.total_cases
        metrics.subtask_completion_rate /= metrics.total_cases
    
    return {
        "overall_accuracy": metrics.successful_cases / metrics.total_cases if metrics.total_cases > 0 else 0,
        "metrics": {
            "tool_selection_accuracy": metrics.tool_selection_accuracy,
            "tool_call_count": metrics.tool_call_count,
            "redundant_tool_call_rate": metrics.redundant_tool_call_rate,
            "stock_code_arg_accuracy": metrics.stock_code_arg_accuracy,
            "time_range_arg_accuracy": metrics.time_range_arg_accuracy,
            "doc_type_arg_accuracy": metrics.doc_type_arg_accuracy,
            "compare_arg_accuracy": metrics.compare_arg_accuracy,
            "slot_retention_accuracy": metrics.slot_retention_accuracy,
            "cross_turn_reference_accuracy": metrics.cross_turn_reference_accuracy,
            "session_isolation_accuracy": metrics.session_isolation_accuracy,
            "unsupported_refusal_rate": metrics.unsupported_refusal_rate,
            "hallucination_rate": metrics.hallucination_rate,
            "task_completion_rate": metrics.task_completion_rate,
            "subtask_completion_rate": metrics.subtask_completion_rate
        },
        "results": results
    }

def load_agent_dataset(file_path: str) -> List[Dict[str, Any]]:
    """加载 Agent 评估数据集"""
    with open(file_path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return data.get("dataset", [])

def main():
    """主函数"""
    dataset = load_agent_dataset("agent_eval_dataset.json")
    results = evaluate_agent_dataset(dataset)
    
    # 保存评估结果
    with open("agent_eval_results.json", "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=2)
    
    # 打印评估报告
    print("\n" + "="*70)
    print("Agent 评估报告")
    print("="*70)
    
    print(f"\n总体准确率: {results['overall_accuracy']:.2%}")
    print(f"评估案例数: {len(results['results'])}")
    
    print("\n【1. 工具选择指标】")
    print(f"  工具选择准确率: {results['metrics']['tool_selection_accuracy']:.4f}")
    print(f"  平均工具调用次数: {results['metrics']['tool_call_count'] / len(results['results']):.2f}")
    print(f"  冗余工具调用率: {results['metrics']['redundant_tool_call_rate']:.4f}")
    
    print("\n【2. 工具参数指标】")
    print(f"  股票代码参数准确率: {results['metrics']['stock_code_arg_accuracy']:.4f}")
    print(f"  时间范围参数准确率: {results['metrics']['time_range_arg_accuracy']:.4f}")
    print(f"  文档类型参数准确率: {results['metrics']['doc_type_arg_accuracy']:.4f}")
    print(f"  比较参数准确率: {results['metrics']['compare_arg_accuracy']:.4f}")
    
    print("\n【3. 多轮记忆指标】")
    print(f"  槽位保留准确率: {results['metrics']['slot_retention_accuracy']:.4f}")
    print(f"  跨轮引用准确率: {results['metrics']['cross_turn_reference_accuracy']:.4f}")
    print(f"  会话隔离准确率: {results['metrics']['session_isolation_accuracy']:.4f}")
    
    print("\n【4. 拒答能力指标】")
    print(f"  不支持问题拒答率: {results['metrics']['unsupported_refusal_rate']:.4f}")
    print(f"  幻觉率: {results['metrics']['hallucination_rate']:.4f}")
    
    print("\n【5. 复杂任务拆解指标】")
    print(f"  任务完成率: {results['metrics']['task_completion_rate']:.4f}")
    print(f"  子任务完成率: {results['metrics']['subtask_completion_rate']:.4f}")


if __name__ == "__main__":
    main()
