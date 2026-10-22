"""
PM Planner Agent Pod — turns an idea into a project plan.

Tools:
  - plan(brief, context_files?) → a Plan, version 1
  - revise(plan, feedback, context_files?) → the next version
  - ping

The plan comes back as a function call, not as prose. Every other pod in this
repo asks the model for text and hands it on; this one needs a document with
named parts that a screen can render and a person can argue with, and parsing
that out of markdown is a losing game. `deliver_plan` is declared as a tool and
the model is required to call it, so the shape is the model provider's problem
rather than ours.

What is still ours is everything that can go wrong after that, and the three
defences here were each taken from somewhere that had already been bitten:

  * read the tool call BY NAME — taking tool_calls[0] loses a deliver_plan that
    arrives second (CodeZard's `_plan_or_questions`);
  * validate field by field, because there is no pydantic or jsonschema in this
    repo and a half-filled plan looks exactly like a full one until someone
    reads it (`research_agent/hebbian/extractor.py`);
  * one retry, carrying the reason, because a model told WHY it failed usually
    fixes it and a model asked again blindly usually repeats itself.
"""

import json
import os
import sys

from openai import OpenAI

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from prompts import (  # noqa: E402
    PLAN_INSTRUCTION,
    REVISE_INSTRUCTION,
    RETRY_PREFIX,
    SYSTEM,
)

API_KEY = os.environ.get("OPENROUTER_API_KEY", "")
LLM_BASE_URL = os.environ.get("LLM_BASE_URL", "https://openrouter.ai/api/v1")
MODEL = os.environ.get("LLM_MODEL", "google/gemini-2.0-flash-001")

#: A plan runs long: objective, two scope lists, milestones, tasks with
#: dependencies, risks, open questions and done criteria, in the user's
#: language. Measured against minimax free at 2000: the JSON was cut off at
#: 3181 characters, mid-task, and arrived as a tool call that would not parse.
#: Truncation is the most likely failure here and the quietest, so the ceiling
#: is generous and `length` is checked for explicitly below.
MAX_TOKENS = int(os.environ.get("PM_MAX_TOKENS", "8000"))

#: Low, not zero. A plan is a judgement, and the same brief twice should not
#: produce two unrelated plans.
TEMPERATURE = float(os.environ.get("PM_TEMPERATURE", "0.3"))

#: Context budget. Generous, since the models in use have six-figure context
#: windows, but not unbounded: the point of a limit is that going over it is
#: reported rather than silently truncating the last file.
MAX_CONTEXT_CHARS = int(os.environ.get("PM_MAX_CONTEXT_CHARS", "200000"))
MAX_FILE_CHARS = int(os.environ.get("PM_MAX_FILE_CHARS", "60000"))

MIN_BRIEF_CHARS = 12


# ---------------------------------------------------------------------------
# The tool: what shape a plan has
# ---------------------------------------------------------------------------

_STRINGS = {"type": "array", "items": {"type": "string"}}

PLAN_TOOL = {
    "type": "function",
    "function": {
        "name": "deliver_plan",
        "description": "Deliver the project plan.",
        "parameters": {
            "type": "object",
            "required": [
                "objective", "scope", "milestones", "tasks",
                "risks", "openQuestions", "doneCriteria",
            ],
            "properties": {
                "objective": {
                    "type": "string",
                    "description": "What this project is for, in one or two sentences.",
                },
                "scope": {
                    "type": "object",
                    "required": ["included", "excluded"],
                    "properties": {
                        "included": _STRINGS,
                        "excluded": {
                            **_STRINGS,
                            "description": (
                                "What a reader would reasonably assume is included "
                                "and is NOT. Empty means you did not think about "
                                "the boundary."
                            ),
                        },
                    },
                },
                "milestones": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "required": ["name", "outcome"],
                        "properties": {
                            "name": {"type": "string"},
                            "outcome": {
                                "type": "string",
                                "description": "Something a person could look at and confirm happened.",
                            },
                        },
                    },
                },
                "tasks": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "required": ["id", "title", "milestone"],
                        "properties": {
                            "id": {"type": "string", "description": "Short, e.g. t1."},
                            "title": {"type": "string"},
                            "milestone": {"type": "string", "description": "Name of its milestone."},
                            "dependsOn": {
                                **_STRINGS,
                                "description": "Task ids this one waits for. Empty means it can start now.",
                            },
                        },
                    },
                },
                "risks": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "required": ["statement", "mitigation"],
                        "properties": {
                            "statement": {"type": "string"},
                            "mitigation": {"type": "string"},
                        },
                    },
                },
                "openQuestions": {
                    **_STRINGS,
                    "description": (
                        "Decisions you could not make from what you were told. "
                        "Empty is a strong claim and almost never true."
                    ),
                },
                "doneCriteria": {
                    **_STRINGS,
                    "description": "How anyone can tell the work is finished.",
                },
                "entities": {
                    "type": "array",
                    "description": "Only for software projects. Omit otherwise.",
                    "items": {
                        "type": "object",
                        "required": ["name", "description"],
                        "properties": {
                            "name": {"type": "string"},
                            "description": {"type": "string"},
                        },
                    },
                },
                "flows": {
                    "type": "array",
                    "description": "Only for software projects. Omit otherwise.",
                    "items": {
                        "type": "object",
                        "required": ["name", "steps"],
                        "properties": {"name": {"type": "string"}, "steps": _STRINGS},
                    },
                },
                "constraints": {
                    "type": "array",
                    "description": "Only for software projects. Omit otherwise.",
                    "items": {
                        "type": "object",
                        "required": ["kind", "statement"],
                        "properties": {
                            "kind": {
                                "type": "string",
                                "enum": ["performance", "security", "compatibility", "other"],
                            },
                            "statement": {"type": "string"},
                        },
                    },
                },
            },
        },
    },
}


class PlanError(Exception):
    """Something the caller should see, with the reason attached."""


# ---------------------------------------------------------------------------
# Context files
# ---------------------------------------------------------------------------

def _render_context(files):
    """Turn the caller's files into prompt text, and say what was left out.

    Returns `(text, skipped)`. `skipped` is a list of `{name, reason}` and it
    travels all the way back to the screen: a file that could not be read is a
    hole in what the plan rests on, and the person who wrote the brief is the
    only one who can tell whether it mattered.
    """
    if not files:
        return "", []

    parts, skipped, budget = [], [], MAX_CONTEXT_CHARS

    for entry in files:
        if not isinstance(entry, dict):
            skipped.append({"name": str(entry)[:80], "reason": "not a file object"})
            continue
        name = str(entry.get("name") or "unnamed")
        content = entry.get("content")
        if content is None:
            skipped.append({"name": name, "reason": entry.get("reason") or "no content"})
            continue
        if not isinstance(content, str):
            skipped.append({"name": name, "reason": "content is not text"})
            continue
        if not content.strip():
            skipped.append({"name": name, "reason": "empty"})
            continue
        if len(content) > MAX_FILE_CHARS:
            skipped.append({
                "name": name,
                "reason": f"{len(content)} characters, over the {MAX_FILE_CHARS} limit for one file",
            })
            continue
        if len(content) > budget:
            skipped.append({
                "name": name,
                "reason": f"the {MAX_CONTEXT_CHARS}-character context budget was already full",
            })
            continue
        budget -= len(content)
        parts.append(f"=== FILE: {name} ===\n{content}")

    if not parts:
        return "", skipped

    text = "=== CONTEXT PROVIDED BY THE USER ===\n\n" + "\n\n".join(parts)
    if skipped:
        names = ", ".join(s["name"] for s in skipped)
        # The model is told too, not just the screen. A plan written as if it
        # had read everything, when it had not, is the failure this prevents.
        text += (
            f"\n\n=== NOT READ ===\nThese files were provided but could not be "
            f"included: {names}. Do not assume what they contain."
        )
    return text, skipped


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

def _texts(value):
    """A list of non-empty strings, or an empty list. Never raises."""
    if not isinstance(value, list):
        return []
    return [s.strip() for s in value if isinstance(s, str) and s.strip()]


def _objects(value, required):
    """Objects that have every required key as a non-empty string."""
    if not isinstance(value, list):
        return []
    kept = []
    for item in value:
        if not isinstance(item, dict):
            continue
        if all(isinstance(item.get(k), str) and item[k].strip() for k in required):
            kept.append(item)
    return kept


def _validate(raw):
    """Turn the model's arguments into a plan, or say why they are not one.

    Checked by hand rather than with a schema library because this repo has
    neither pydantic nor jsonschema, and adding a dependency for one call site
    would be the larger change. `hebbian/extractor.py` validates the same way.
    """
    if not isinstance(raw, dict):
        raise PlanError("the model's answer was not an object")

    objective = raw.get("objective")
    if not isinstance(objective, str) or not objective.strip():
        raise PlanError("the plan has no objective")

    scope_raw = raw.get("scope") if isinstance(raw.get("scope"), dict) else {}
    scope = {
        "included": _texts(scope_raw.get("included")),
        "excluded": _texts(scope_raw.get("excluded")),
    }
    milestones = _objects(raw.get("milestones"), ("name", "outcome"))
    risks = _objects(raw.get("risks"), ("statement", "mitigation"))
    done = _texts(raw.get("doneCriteria"))

    tasks = []
    for t in _objects(raw.get("tasks"), ("id", "title")):
        tasks.append({
            "id": t["id"].strip(),
            "title": t["title"].strip(),
            "milestone": (t.get("milestone") or "").strip(),
            "dependsOn": _texts(t.get("dependsOn")),
        })

    # What makes a plan actionable, named one by one so the retry can say which
    # part is missing. CodeZard rejects on a similar condition; this one is
    # stricter, and for a measured reason: the first real run came back with a
    # detailed scope — eleven items in, eight out — and then empty milestones,
    # tasks, risks and doneCriteria. It passed an "objective OR scope OR tasks"
    # check and would have been shown to someone as a plan. A document that
    # says what a project covers but not what to do about it is a scope note.
    missing = []
    if not milestones:
        missing.append("milestones")
    if not tasks:
        missing.append("tasks")
    if not done:
        missing.append("doneCriteria")
    if missing:
        raise PlanError(
            "the plan is missing " + ", ".join(missing)
            + " — a plan without those is not actionable"
        )

    plan = {
        "version": 1,
        "objective": objective.strip(),
        "scope": scope,
        "milestones": milestones,
        "tasks": tasks,
        "risks": risks,
        "openQuestions": _texts(raw.get("openQuestions")),
        "doneCriteria": done,
        "status": "draft",
    }

    # Software-only sections. Absent is the normal case; empty lists would
    # suggest the model considered them and found none, which is different.
    if entities := _objects(raw.get("entities"), ("name", "description")):
        plan["entities"] = [{"name": e["name"], "description": e["description"]} for e in entities]
    if flows := _objects(raw.get("flows"), ("name",)):
        kept = [{"name": f["name"], "steps": _texts(f.get("steps"))} for f in flows]
        if any(f["steps"] for f in kept):
            plan["flows"] = kept
    if isinstance(raw.get("constraints"), list):
        kinds = {"performance", "security", "compatibility", "other"}
        cons = []
        for c in _objects(raw["constraints"], ("statement",)):
            kind = c.get("kind")
            cons.append({
                # Anything outside the enum collapses to "other" rather than
                # being dropped: the statement is the part that matters.
                "kind": kind if kind in kinds else "other",
                "statement": c["statement"],
            })
        if cons:
            plan["constraints"] = cons

    if not plan["openQuestions"]:
        plan["openQuestions"] = [
            "This plan declared nothing open, which is almost never true. "
            "Read it assuming something was decided without being stated."
        ]

    return plan


# ---------------------------------------------------------------------------
# Calling the model
# ---------------------------------------------------------------------------

def _extract(message):
    """The arguments of the deliver_plan call, by name.

    By name and not by position: a model that emits two calls does not promise
    an order, and indexing the first one silently drops a plan that arrived
    second.
    """
    calls = getattr(message, "tool_calls", None) or []
    for call in calls:
        if getattr(call.function, "name", "") != "deliver_plan":
            continue
        raw = call.function.arguments or ""
        try:
            return json.loads(raw)
        except json.JSONDecodeError as exc:
            # Nearly always truncation. Saying so points at max_tokens instead
            # of sending someone to look for a malformed prompt.
            raise PlanError(
                f"the model's plan was not valid JSON ({exc}); it is "
                f"{len(raw)} characters and was probably cut off — raise "
                f"PM_MAX_TOKENS (currently {MAX_TOKENS})"
            ) from exc
    text = (getattr(message, "content", "") or "").strip()
    raise PlanError(
        "the model answered without calling deliver_plan"
        + (f": {text[:200]}" if text else "")
    )


def _ask(client, instruction, user_content):
    """One call, and one retry that carries the reason the first was rejected."""
    if not API_KEY:
        raise PlanError(
            "OPENROUTER_API_KEY is not set, so there is no model to plan with"
        )

    messages = [
        {"role": "system", "content": f"{SYSTEM}\n\n{instruction}"},
        {"role": "user", "content": user_content},
    ]

    last = None
    for attempt in (1, 2):
        try:
            response = client.chat.completions.create(
                model=MODEL,
                messages=messages,
                tools=[PLAN_TOOL],
                tool_choice={"type": "function", "function": {"name": "deliver_plan"}},
                temperature=TEMPERATURE,
                max_tokens=MAX_TOKENS,
            )
        except Exception as exc:
            # The provider's own words. "the model is unavailable" tells the
            # person nothing they can act on; a rate limit or a dead model id
            # tells them exactly what to change.
            raise PlanError(f"the model provider refused the request: {exc}") from exc

        choice = response.choices[0]
        if choice.finish_reason == "length":
            raise PlanError(
                f"the model hit its {MAX_TOKENS}-token ceiling before finishing "
                f"the plan — raise PM_MAX_TOKENS"
            )
        try:
            return _validate(_extract(choice.message))
        except PlanError as exc:
            last = exc
            if attempt == 2:
                break
            messages.append({"role": "assistant", "content": str(choice.message.content or "")})
            messages.append({"role": "user", "content": RETRY_PREFIX.format(reason=exc)})

    raise PlanError(f"the model failed twice: {last}")


# ---------------------------------------------------------------------------
# Tools
# ---------------------------------------------------------------------------

def handle_plan(client, params):
    brief = (params.get("brief") or params.get("idea") or params.get("message") or "").strip()
    if len(brief) < MIN_BRIEF_CHARS:
        # Rejected here rather than sent to the model. A three-word brief comes
        # back as a plan about nothing, which reads like the product working.
        return {"error": f"the brief is too short — describe the idea in at least {MIN_BRIEF_CHARS} characters"}

    context, skipped = _render_context(params.get("context_files"))
    user = f"{context}\n\n=== THE IDEA ===\n{brief}" if context else f"=== THE IDEA ===\n{brief}"

    try:
        plan = _ask(client, PLAN_INSTRUCTION, user)
    except PlanError as exc:
        return {"error": str(exc)}
    return {"plan": plan, "skipped_files": skipped}


def handle_revise(client, params):
    previous = params.get("plan")
    if not isinstance(previous, dict) or not previous.get("objective"):
        return {"error": "revise needs the plan being revised"}
    feedback = (params.get("feedback") or "").strip()
    if not feedback:
        return {"error": "revise needs the feedback that rejected the plan"}

    context, skipped = _render_context(params.get("context_files"))
    # The previous plan goes in as readable text, not as JSON. A model reads a
    # document better than it reads a serialisation of one, and this is the
    # input it is being asked to reconsider rather than to parse.
    user = "\n\n".join(filter(None, [
        context,
        "=== THE PLAN THEY REJECTED ===\n" + _render_plan(previous),
        "=== WHAT THEY SAID ===\n" + feedback,
    ]))

    try:
        plan = _ask(client, REVISE_INSTRUCTION, user)
    except PlanError as exc:
        return {"error": str(exc)}

    version = previous.get("version")
    plan["version"] = (version + 1) if isinstance(version, int) and version > 0 else 2
    plan["revisionOf"] = {"version": version if isinstance(version, int) else 1,
                          "feedback": feedback}
    return {"plan": plan, "skipped_files": skipped}


def _render_plan(plan):
    """The plan as prose, for the model to read back."""
    out = [f"Objective: {plan.get('objective', '')}"]
    scope = plan.get("scope") or {}
    if inc := scope.get("included"):
        out.append("In scope:\n" + "\n".join(f"- {s}" for s in inc))
    if exc := scope.get("excluded"):
        out.append("Out of scope:\n" + "\n".join(f"- {s}" for s in exc))
    if ms := plan.get("milestones"):
        out.append("Milestones:\n" + "\n".join(
            f"- {m.get('name')}: {m.get('outcome')}" for m in ms))
    if ts := plan.get("tasks"):
        out.append("Tasks:\n" + "\n".join(
            f"- [{t.get('id')}] {t.get('title')}"
            + (f" (after {', '.join(t['dependsOn'])})" if t.get("dependsOn") else "")
            for t in ts))
    if rs := plan.get("risks"):
        out.append("Risks:\n" + "\n".join(
            f"- {r.get('statement')} → {r.get('mitigation')}" for r in rs))
    if oq := plan.get("openQuestions"):
        out.append("Open questions:\n" + "\n".join(f"- {q}" for q in oq))
    if dc := plan.get("doneCriteria"):
        out.append("Done when:\n" + "\n".join(f"- {d}" for d in dc))
    return "\n\n".join(out)


# ---------------------------------------------------------------------------
# JSON-RPC over stdio
# ---------------------------------------------------------------------------

def _process_request(client, line):
    line = line.strip()
    if not line:
        return
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        return

    req_id = req.get("id", 0)
    method = req.get("method", "")
    params = req.get("params", {})
    if isinstance(params, str):
        try:
            params = json.loads(params)
        except json.JSONDecodeError:
            params = {"brief": params}
    if not isinstance(params, dict):
        params = {}

    resp = {"jsonrpc": "2.0", "id": req_id}
    try:
        if method == "ping":
            resp["result"] = {"status": "ok", "agent": "pm-planner", "model": MODEL}
        elif method == "plan":
            resp["result"] = handle_plan(client, params)
        elif method == "revise":
            resp["result"] = handle_revise(client, params)
        else:
            resp["error"] = {"code": -32601, "message": f"method not found: {method}"}
    except Exception as exc:  # noqa: BLE001 — a pod that dies stops serving
        resp["error"] = {"code": -32000, "message": str(exc)}

    sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def main():
    client = OpenAI(base_url=LLM_BASE_URL, api_key=API_KEY or "lm-studio")
    for line in sys.stdin:
        _process_request(client, line)


if __name__ == "__main__":
    main()
