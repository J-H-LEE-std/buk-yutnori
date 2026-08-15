#!/usr/bin/env python3
"""Validate canonical YAML, JSON Schema examples, and the board graph."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

import yaml


ROOT = Path(__file__).resolve().parents[1]
SCHEMAS = ROOT / "schemas"


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def load_yaml(path: Path) -> Any:
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def validate_parse() -> None:
    yaml_paths = sorted(
        [
            *ROOT.joinpath("spec").glob("*.yaml"),
            *ROOT.joinpath(".github", "workflows").glob("*.yml"),
            *ROOT.joinpath(".github", "workflows").glob("*.yaml"),
            *ROOT.joinpath(".github", "ISSUE_TEMPLATE").glob("*.yml"),
            *ROOT.joinpath(".github", "ISSUE_TEMPLATE").glob("*.yaml"),
        ]
    )
    json_paths = sorted(SCHEMAS.rglob("*.json"))

    for path in yaml_paths:
        load_yaml(path)
    for path in json_paths:
        load_json(path)

    print(f"PARSE_OK yaml={len(yaml_paths)} json={len(json_paths)}")


def validator_for(schema_path: Path) -> Any:
    from jsonschema import Draft202012Validator, RefResolver

    schema = load_json(schema_path)
    Draft202012Validator.check_schema(schema)
    resolver = RefResolver(
        base_uri=schema_path.resolve().as_uri(),
        referrer=schema,
    )
    return Draft202012Validator(schema, resolver=resolver)


def validate_contracts() -> None:
    from jsonschema import Draft202012Validator

    schema_paths = sorted(SCHEMAS.glob("*.schema.json"))
    for schema_path in schema_paths:
        Draft202012Validator.check_schema(load_json(schema_path))

    cases = [
        ("ws_client_command.schema.json", "client_commands.json"),
        ("ws_server_response.schema.json", "server_responses.json"),
        ("ws_server_event.schema.json", "server_events.json"),
        ("game_snapshot.schema.json", "game_snapshot.json"),
        ("http_auth.schema.json", "http_auth.json"),
    ]
    counts: list[str] = []

    for schema_name, example_name in cases:
        validator = validator_for(SCHEMAS / schema_name)
        examples = load_json(SCHEMAS / "examples" / example_name)
        if not isinstance(examples, list):
            examples = [examples]
        for index, example in enumerate(examples):
            errors = sorted(validator.iter_errors(example), key=lambda error: list(error.path))
            if errors:
                details = "; ".join(
                    f"{'/'.join(map(str, error.path)) or '<root>'}: {error.message}"
                    for error in errors
                )
                raise AssertionError(f"{example_name}[{index}] failed: {details}")
        counts.append(f"{example_name}={len(examples)}")

    room_validator = validator_for(SCHEMAS / "room_settings.schema.json")
    room_settings = load_yaml(ROOT / "spec" / "room_settings.yaml")
    room_errors = sorted(
        room_validator.iter_errors(room_settings["defaults"]),
        key=lambda error: list(error.path),
    )
    if room_errors:
        details = "; ".join(error.message for error in room_errors)
        raise AssertionError(f"room_settings.yaml defaults failed: {details}")

    room_schema = load_json(SCHEMAS / "room_settings.schema.json")
    schema_props = room_schema["properties"]
    for key, allowed_values in room_settings["allowed"].items():
        if "enum" not in schema_props[key]:
            continue
        if allowed_values != schema_props[key]["enum"]:
            raise AssertionError(f"room_settings allowed values differ for {key}")

    turn_state_machine = load_yaml(ROOT / "spec" / "turn_state_machine.yaml")
    ordinary_movement_spaces = turn_state_machine["queue"]["token"].get(
        "ordinary_movement_spaces"
    )
    expected_ordinary_movement_spaces = {
        "do": 1,
        "gae": 2,
        "geol": 3,
        "yut": 4,
        "mo": 5,
    }
    if ordinary_movement_spaces != expected_ordinary_movement_spaces:
        raise AssertionError(
            "ordinary movement spaces must be "
            f"{expected_ordinary_movement_spaces}, got {ordinary_movement_spaces}"
        )
    retry_delays = turn_state_machine["persistence"].get("retry_delays_seconds")
    if retry_delays != [1, 2, 5]:
        raise AssertionError("storage retry delays must be [1, 2, 5] seconds")
    if turn_state_machine["persistence"].get("retries_after_initial_failure") != len(retry_delays):
        raise AssertionError("storage retry count must match retry_delays_seconds")

    print(
        f"CONTRACTS_OK schemas={len(schema_paths)} "
        + " ".join(counts)
        + " room_settings.yaml=1"
        + " ordinary_movement_spaces=do:1,gae:2,geol:3,yut:4,mo:5"
        + " storage_retry_delays=1,2,5"
    )


def validate_board() -> None:
    board = load_yaml(ROOT / "spec" / "board_graph.yaml")
    nodes = board["nodes"]
    node_ids = [node["id"] for node in nodes]
    node_set = set(node_ids)
    edges = [tuple(edge) for edge in board["forward_edges"]]

    if len(node_ids) != len(node_set):
        raise AssertionError("board node IDs are not unique")

    for source, destination in edges:
        if source not in node_set or destination not in node_set:
            raise AssertionError(f"invalid edge reference: {source} -> {destination}")

    edge_set = set(edges)
    for source, choices in board["route_choices"].items():
        if source not in node_set:
            raise AssertionError(f"invalid route source: {source}")
        for destination in choices.values():
            if (source, destination) not in edge_set:
                raise AssertionError(
                    f"route choice is not a forward edge: {source} -> {destination}"
                )

    tagged = {
        node["id"]
        for node in nodes
        if "buk_candidate" in node.get("tags", [])
    }
    explicit = set(board["buk"]["random_candidates"])
    if tagged != explicit:
        raise AssertionError(
            f"buk candidates differ: tagged={sorted(tagged)} explicit={sorted(explicit)}"
        )
    if len(tagged) != 10:
        raise AssertionError(f"expected 10 buk candidates, got {len(tagged)}")
    route_choice_nodes = set(board["route_choices"])
    if tagged & route_choice_nodes:
        raise AssertionError(
            f"buk candidates require route choice: {sorted(tagged & route_choice_nodes)}"
        )

    adjacency = {node_id: [] for node_id in node_set}
    for source, destination in edges:
        adjacency[source].append(destination)

    start = board["start_space"]
    reachable = {start}
    pending = [start]
    while pending:
        for destination in adjacency[pending.pop()]:
            if destination not in reachable:
                reachable.add(destination)
                pending.append(destination)

    if reachable != node_set:
        raise AssertionError(f"unreachable nodes: {sorted(node_set - reachable)}")

    for origin in node_set:
        can_finish = {origin}
        pending = [origin]
        while pending:
            for destination in adjacency[pending.pop()]:
                if destination not in can_finish:
                    can_finish.add(destination)
                    pending.append(destination)
        if board["home_checkpoint_space"] not in can_finish:
            raise AssertionError(f"no forward finish path from: {origin}")

    coordinates = set(board["render_reference"]["coordinates"])
    if coordinates != node_set:
        raise AssertionError(
            "render coordinates differ from nodes: "
            f"missing={sorted(node_set - coordinates)} "
            f"extra={sorted(coordinates - node_set)}"
        )

    print(
        "BOARD_OK "
        f"nodes={len(nodes)} edges={len(edges)} "
        f"reachable={len(reachable)} buk={len(tagged)}"
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "scope",
        choices=("all", "parse", "contracts", "board"),
        nargs="?",
        default="all",
    )
    args = parser.parse_args()

    if args.scope in ("all", "parse"):
        validate_parse()
    if args.scope in ("all", "contracts"):
        validate_contracts()
    if args.scope in ("all", "board"):
        validate_board()


if __name__ == "__main__":
    main()
