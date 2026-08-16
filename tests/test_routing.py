from sleepyrouter.routing import (
    all_group_model_ids,
    candidate_ids,
    normalize_model_group_name,
    normalize_model_groups_ordered,
    resolve_default_group,
)


def test_normalize_model_group_name() -> None:
    assert normalize_model_group_name("  FAST ") == "fast"
    assert normalize_model_group_name("") == ""


def test_normalize_model_groups_ordered() -> None:
    raw = {"capable": ["capable-1", "capable-2"], "fast": ["fast-1"]}
    groups, order = normalize_model_groups_ordered(raw)
    assert groups == {"capable": ["capable-1", "capable-2"], "fast": ["fast-1"]}
    assert order == ["capable", "fast"]


def test_all_group_model_ids() -> None:
    groups = {
        "fast": ["model-a", "model-b"],
        "balanced": ["model-b", "model-c"],
        "capable": ["model-d"],
    }
    ids = all_group_model_ids(groups, "balanced", "fast", "capable")
    assert ids == ["model-b", "model-c", "model-a", "model-d"]


def test_resolve_default_group() -> None:
    groups = {"fast": ["a"], "balanced": ["b"]}
    assert resolve_default_group(groups, "balanced") == "balanced"
    assert resolve_default_group(groups, "invalid", "fast") == "fast"


def test_candidate_ids() -> None:
    groups = {"fast": ["fast-1", "fast-2"], "balanced": ["bal-1"]}
    ids, reason = candidate_ids(groups, "FAST", "balanced")
    assert ids == ["fast-1", "fast-2"]
    assert reason == "model-group"

    ids2, reason2 = candidate_ids(groups, "unknown", "balanced")
    assert ids2 == ["bal-1"]
    assert reason2 == "fallback-order"


def test_candidate_ids_direct_model() -> None:
    groups = {"fast": ["fast-1", "fast-2"], "balanced": ["bal-1"]}
    ids, reason = candidate_ids(groups, "fast-2", "balanced")
    assert ids == ["fast-2"]
    assert reason == "direct-model"
