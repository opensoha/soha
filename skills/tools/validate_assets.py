#!/usr/bin/env python3
"""Validate OpenSoha skills, MCP presets, agent profiles, and skill index."""

from __future__ import annotations

import argparse
import datetime
import gzip
import hashlib
import json
import os
import re
import shutil
import sys
import tarfile
import tempfile
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "schemas"
CATALOG_PATH = ROOT / "catalog" / "gateway-capabilities.json"
PLATFORM_CATALOG_PATH = ROOT / "catalog" / "platform-capabilities.json"
AI_PLATFORM_CATALOG_PATH = ROOT / "catalog" / "ai-platform-capabilities.json"
COMPATIBILITY_MATRIX_PATH = ROOT / "catalog" / "compatibility-matrix.json"
ASSET_GOVERNANCE_PATH = ROOT / "catalog" / "asset-governance.json"
CATALOG_README_PATH = ROOT / "catalog" / "README.md"
AGENT_SKILLS_ROOT = ROOT / "agent-skills"
SOURCE_CONTRACTS_ROOT = ROOT.parent / "contracts"
CONTRACT_SCHEMA_RELATIVE_PATHS = {
    "skill": "skills/skill-manifest.schema.json",
    "mcpPreset": "presets/mcp-preset.schema.json",
    "agentProfile": "profiles/agent-profile.schema.json",
}
LOCAL_SCHEMA_FILES = {
    "skill": "skill-frontmatter.schema.json",
    "mcpPreset": "mcp-preset.schema.json",
    "agentProfile": "agent-profile.schema.json",
}
DEFAULT_GATEWAY_CATALOG_SOURCE = ROOT.parent / "internal" / "application" / "aigateway" / "catalog.go"
DEFAULT_PLATFORM_CAPABILITY_SOURCE = ROOT.parent / "internal" / "domain" / "cluster" / "capabilities.go"
REQUIRED_SKILL_SECTIONS = ("Operating Contract", "Workflow", "Examples", "Permission Boundaries", "Forbidden Actions", "Guardrails")
ALLOWED_SKILL_CATEGORIES = {"delivery", "platform", "security"}
RELEASE_INCLUDE_DIRS = ("agent-profiles", "agent-skills", "catalog", "mcp-presets", "schemas", "skills")
RELEASE_INCLUDE_FILES = ("LICENSE", "README.md")
SENSITIVE_TERMS_RE = re.compile(
    r"\b(access token|refresh token|token|kubeconfig|password|private key|secret|credential|registry credential|environment variable)\b",
    re.IGNORECASE,
)
SECRET_ASSIGNMENT_RE = re.compile(
    r"(?i)\b(token|password|secret|private[_ -]?key|credential)\b\s*[:=]\s*['\"]?[A-Za-z0-9_./+=-]{8,}"
)


class ValidationError(Exception):
    pass


class SimpleYAMLParser:
    def __init__(self, text: str, source: Path):
        self.source = source
        self.lines: list[tuple[int, int, str]] = []
        for number, raw in enumerate(text.splitlines(), 1):
            if "\t" in raw:
                raise ValidationError(f"{source}:{number}: tabs are not allowed in YAML")
            stripped = raw.strip()
            if not stripped or stripped.startswith("#"):
                continue
            indent = len(raw) - len(raw.lstrip(" "))
            if indent % 2 != 0:
                raise ValidationError(f"{source}:{number}: indentation must use two-space steps")
            self.lines.append((number, indent, raw[indent:]))

    def parse(self) -> Any:
        if not self.lines:
            return {}
        value, index = self._parse_block(0, self.lines[0][1])
        if index != len(self.lines):
            number, _, _ = self.lines[index]
            raise ValidationError(f"{self.source}:{number}: unexpected trailing YAML content")
        return value

    def _parse_block(self, index: int, indent: int) -> tuple[Any, int]:
        if index >= len(self.lines):
            return {}, index
        number, current_indent, content = self.lines[index]
        if current_indent < indent:
            return {}, index
        if current_indent > indent:
            raise ValidationError(f"{self.source}:{number}: unexpected indentation")
        if content.startswith("- "):
            return self._parse_sequence(index, indent)
        return self._parse_mapping(index, indent)

    def _parse_mapping(self, index: int, indent: int) -> tuple[dict[str, Any], int]:
        out: dict[str, Any] = {}
        while index < len(self.lines):
            number, current_indent, content = self.lines[index]
            if current_indent < indent:
                break
            if current_indent > indent:
                raise ValidationError(f"{self.source}:{number}: unexpected nested mapping content")
            if content.startswith("- "):
                break
            key, raw_value = self._split_key_value(number, content)
            if key in out:
                raise ValidationError(f"{self.source}:{number}: duplicate YAML key {key!r}")
            if raw_value == "":
                value, index = self._parse_block(index + 1, indent + 2)
            else:
                value = self._parse_scalar(number, raw_value)
                index += 1
            out[key] = value
        return out, index

    def _parse_sequence(self, index: int, indent: int) -> tuple[list[Any], int]:
        out: list[Any] = []
        while index < len(self.lines):
            number, current_indent, content = self.lines[index]
            if current_indent < indent:
                break
            if current_indent > indent:
                raise ValidationError(f"{self.source}:{number}: unexpected nested sequence content")
            if not content.startswith("- "):
                break
            raw_item = content[2:].strip()
            if raw_item == "":
                value, index = self._parse_block(index + 1, indent + 2)
                out.append(value)
                continue
            if self._looks_like_key_value(raw_item):
                key, raw_value = self._split_key_value(number, raw_item)
                item: dict[str, Any] = {}
                if raw_value == "":
                    value, next_index = self._parse_block(index + 1, indent + 2)
                    index = next_index
                else:
                    value = self._parse_scalar(number, raw_value)
                    index += 1
                item[key] = value
                if index < len(self.lines):
                    next_number, next_indent, _ = self.lines[index]
                    if next_indent > indent:
                        child, index = self._parse_block(index, indent + 2)
                        if not isinstance(child, dict):
                            raise ValidationError(f"{self.source}:{next_number}: list item continuation must be a mapping")
                        for child_key, child_value in child.items():
                            if child_key in item:
                                raise ValidationError(f"{self.source}:{next_number}: duplicate YAML key {child_key!r}")
                            item[child_key] = child_value
                out.append(item)
                continue
            value = self._parse_scalar(number, raw_item)
            index += 1
            if index < len(self.lines) and self.lines[index][1] > indent:
                next_number = self.lines[index][0]
                raise ValidationError(f"{self.source}:{next_number}: scalar list item cannot have nested content")
            out.append(value)
        return out, index

    def _split_key_value(self, number: int, content: str) -> tuple[str, str]:
        if ":" not in content:
            raise ValidationError(f"{self.source}:{number}: expected key: value")
        key, raw_value = content.split(":", 1)
        key = key.strip()
        if not re.fullmatch(r"[A-Za-z0-9_.-]+", key):
            raise ValidationError(f"{self.source}:{number}: invalid YAML key {key!r}")
        return key, raw_value.strip()

    def _looks_like_key_value(self, value: str) -> bool:
        if ":" not in value:
            return False
        key, _ = value.split(":", 1)
        return re.fullmatch(r"[A-Za-z0-9_.-]+", key.strip()) is not None

    def _parse_scalar(self, number: int, value: str) -> Any:
        if value in {"true", "false"}:
            return value == "true"
        if value == "null":
            return None
        if re.fullmatch(r"-?[0-9]+", value):
            return int(value)
        if (value.startswith('"') and value.endswith('"')) or (value.startswith("'") and value.endswith("'")):
            return value[1:-1]
        if value.startswith(("{", "[", "-")):
            raise ValidationError(f"{self.source}:{number}: unsupported scalar syntax {value!r}")
        return value


def rel(path: Path) -> str:
    try:
        return path.relative_to(ROOT).as_posix()
    except ValueError:
        return path.as_posix()


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text())
    except json.JSONDecodeError as exc:
        raise ValidationError(f"{rel(path)}:{exc.lineno}: invalid JSON: {exc.msg}") from exc


def read_yaml(path: Path) -> Any:
    return SimpleYAMLParser(path.read_text(), path).parse()


def load_schema(name: str) -> dict[str, Any]:
    schema = read_json(SCHEMA_DIR / name)
    if not isinstance(schema, dict):
        raise ValidationError(f"schemas/{name}: schema must be a JSON object")
    return schema


def load_capability_catalog() -> dict[str, Any]:
    catalog = read_json(CATALOG_PATH)
    if not isinstance(catalog, dict):
        raise ValidationError(f"{rel(CATALOG_PATH)}: catalog must be a JSON object")
    validate_schema(catalog, load_schema("gateway-capability-catalog.schema.json"), rel(CATALOG_PATH))
    tool_names: set[str] = set()
    runtime_tool_names: set[str] = set()
    known_skill_refs: set[str] = set(catalog.get("plannedSkills", []))
    resource_names = {resource["name"] for resource in catalog.get("resources", [])}
    prompt_names = {prompt["name"] for prompt in catalog.get("prompts", [])}
    if len(resource_names) != len(catalog.get("resources", [])):
        raise ValidationError(f"{rel(CATALOG_PATH)}: duplicate Gateway resource record")
    if len(prompt_names) != len(catalog.get("prompts", [])):
        raise ValidationError(f"{rel(CATALOG_PATH)}: duplicate Gateway prompt record")

    for tool in catalog["tools"]:
        name = tool["name"]
        if name in tool_names:
            raise ValidationError(f"{rel(CATALOG_PATH)}: duplicate Gateway tool {name!r}")
        tool_names.add(name)
        if tool["status"] == "stable-runtime":
            runtime_tool_names.add(name)
        validate_gateway_catalog_tool_record(tool, resource_names, prompt_names)
        known_skill_refs.update(tool.get("skillRefs", []))

    for resource in catalog.get("resources", []):
        validate_gateway_catalog_resource_record(resource, tool_names, prompt_names)
        known_skill_refs.update(resource.get("skillRefs", []))

    for prompt in catalog.get("prompts", []):
        validate_gateway_catalog_prompt_record(prompt, tool_names, resource_names)
        known_skill_refs.update(prompt.get("skillRefs", []))

    catalog["_runtimeToolNames"] = sorted(runtime_tool_names)
    catalog["_knownSkillRefs"] = sorted(known_skill_refs)
    return catalog


def validate_gateway_catalog_tool_record(
    tool: dict[str, Any], resource_names: set[str], prompt_names: set[str]
) -> None:
    name = tool["name"]
    status = tool["status"]
    if status == "reserved":
        if not name.endswith(".*"):
            raise ValidationError(f"{rel(CATALOG_PATH)}: reserved Gateway namespace {name!r} must end with .*")
        return

    if tool["owningService"] == "reserved":
        raise ValidationError(f"{rel(CATALOG_PATH)}: non-reserved Gateway tool {name!r} must declare an owning service")
    if not tool["permissionKeys"]:
        raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} must declare permissionKeys")
    global_catalog_reads = {"delivery.deployment_templates.list", "delivery.deployment_templates.version"}
    if not tool["requiredScopes"] and not (name in global_catalog_reads and tool["riskLevel"] == "read"):
        raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} must declare requiredScopes")
    if tool["riskLevel"] in {"mutate", "execute", "high"} and not isinstance(tool["requiresApproval"], bool):
        raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} must declare explicit approval posture")
    approval_decision_tools = {"gateway.approvals.decide"}
    if tool["riskLevel"] in {"execute", "high"} and not tool["requiresApproval"] and name not in approval_decision_tools:
        raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} must require approval for {tool['riskLevel']}")
    for resource_ref in tool.get("resourceRefs", []):
        if resource_ref not in resource_names:
            raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} references unknown resource {resource_ref!r}")
    for prompt_ref in tool.get("promptRefs", []):
        if prompt_ref not in prompt_names:
            raise ValidationError(f"{rel(CATALOG_PATH)}: Gateway tool {name!r} references unknown prompt {prompt_ref!r}")


def validate_gateway_catalog_resource_record(
    resource: dict[str, Any], tool_names: set[str], prompt_names: set[str]
) -> None:
    for tool_ref in resource.get("toolRefs", []):
        if tool_ref not in tool_names:
            raise ValidationError(
                f"{rel(CATALOG_PATH)}: Gateway resource {resource['name']!r} references unknown tool {tool_ref!r}"
            )
    for prompt_ref in resource.get("promptRefs", []):
        if prompt_ref not in prompt_names:
            raise ValidationError(
                f"{rel(CATALOG_PATH)}: Gateway resource {resource['name']!r} references unknown prompt {prompt_ref!r}"
            )


def validate_gateway_catalog_prompt_record(
    prompt: dict[str, Any], tool_names: set[str], resource_names: set[str]
) -> None:
    for tool_ref in prompt.get("toolRefs", []):
        if tool_ref not in tool_names:
            raise ValidationError(
                f"{rel(CATALOG_PATH)}: Gateway prompt {prompt['name']!r} references unknown tool {tool_ref!r}"
            )
    for resource_ref in prompt.get("resourceRefs", []):
        if resource_ref not in resource_names:
            raise ValidationError(
                f"{rel(CATALOG_PATH)}: Gateway prompt {prompt['name']!r} references unknown resource {resource_ref!r}"
            )


def load_platform_capability_catalog() -> dict[str, Any]:
    catalog = read_json(PLATFORM_CATALOG_PATH)
    if not isinstance(catalog, dict):
        raise ValidationError(f"{rel(PLATFORM_CATALOG_PATH)}: catalog must be a JSON object")
    validate_schema(catalog, load_schema("platform-capability-catalog.schema.json"), rel(PLATFORM_CATALOG_PATH))
    capability_keys: set[str] = set()
    for capability in catalog["capabilities"]:
        key = capability["key"]
        if key in capability_keys:
            raise ValidationError(f"{rel(PLATFORM_CATALOG_PATH)}: duplicate platform capability {key!r}")
        capability_keys.add(key)
    return catalog


def load_ai_platform_capability_catalog() -> dict[str, Any]:
    catalog = read_json(AI_PLATFORM_CATALOG_PATH)
    if not isinstance(catalog, dict):
        raise ValidationError(f"{rel(AI_PLATFORM_CATALOG_PATH)}: catalog must be a JSON object")
    validate_schema(catalog, load_schema("ai-platform-capability-catalog.schema.json"), rel(AI_PLATFORM_CATALOG_PATH))
    keys: set[str] = set()
    for capability in catalog["capabilities"]:
        key = capability["key"]
        if key in keys:
            raise ValidationError(f"{rel(AI_PLATFORM_CATALOG_PATH)}: duplicate AI platform capability {key!r}")
        keys.add(key)
        if capability["riskLevel"] in {"execute", "high"} and not capability["requiresApproval"]:
            raise ValidationError(f"{rel(AI_PLATFORM_CATALOG_PATH)}: {key!r} must require approval")
    return catalog


def default_contract_schema(relative_path: str) -> Path:
    return SOURCE_CONTRACTS_ROOT / relative_path


def required_source_inputs() -> dict[str, Path]:
    return {
        "contracts_skill_schema": default_contracts_skill_schema(),
        "contracts_mcp_preset_schema": default_contracts_mcp_preset_schema(),
        "contracts_agent_profile_schema": default_contracts_agent_profile_schema(),
        "contracts_permission_catalog": default_contracts_permission_catalog(),
        "gateway_catalog_source": DEFAULT_GATEWAY_CATALOG_SOURCE,
        "platform_capability_source": DEFAULT_PLATFORM_CAPABILITY_SOURCE,
    }


def validate_source_inputs(args: argparse.Namespace) -> None:
    for name, expected in required_source_inputs().items():
        actual = getattr(args, name, None)
        if actual is None or not expected.is_file():
            raise ValidationError(f"required same-source input {name} is missing: {expected}")
        if actual.resolve() != expected.resolve():
            raise ValidationError(f"{name} must use the same-source input {expected}, got {actual}")
        # A redirected producer directory must not masquerade as this source tree.
        if expected.resolve() != expected.absolute():
            raise ValidationError(f"same-source input must not redirect outside its source path: {expected}")


def default_contracts_skill_schema() -> Path:
    return default_contract_schema(CONTRACT_SCHEMA_RELATIVE_PATHS["skill"])


def default_contracts_mcp_preset_schema() -> Path:
    return default_contract_schema(CONTRACT_SCHEMA_RELATIVE_PATHS["mcpPreset"])


def default_contracts_agent_profile_schema() -> Path:
    return default_contract_schema(CONTRACT_SCHEMA_RELATIVE_PATHS["agentProfile"])


def default_contracts_permission_catalog() -> Path:
    return default_contract_schema("auth/permission-catalog.json")


def load_permission_catalog(path: Path) -> tuple[dict[str, Any], set[str]]:
    catalog = read_json(path)
    if not isinstance(catalog, dict) or not isinstance(catalog.get("permissions"), list):
        raise ValidationError(f"{path}: contracts permission catalog must contain a permissions array")
    keys = [permission.get("key") for permission in catalog["permissions"] if isinstance(permission, dict)]
    if any(not isinstance(key, str) or not key for key in keys) or len(keys) != len(catalog["permissions"]):
        raise ValidationError(f"{path}: every permission catalog entry must declare a non-empty key")
    if len(set(keys)) != len(keys):
        raise ValidationError(f"{path}: contracts permission catalog contains duplicate keys")
    return catalog, set(keys)


def validate_catalog_permission_keys(
    source: Path,
    item_kind: str,
    items: list[dict[str, Any]],
    identity_field: str,
    permission_catalog: dict[str, Any],
    known_permission_keys: set[str],
) -> None:
    definitions = {permission["key"]: permission for permission in permission_catalog["permissions"]}
    for item in items:
        unknown = sorted(set(item.get("permissionKeys", [])) - known_permission_keys)
        if unknown:
            raise ValidationError(
                f"{rel(source)}: {item_kind} {item[identity_field]!r} references unknown permission keys {unknown}"
            )
        legacy_manage = sorted(
            key for key in item.get("permissionKeys", []) if definitions[key].get("action") == "manage"
        )
        if legacy_manage:
            raise ValidationError(
                f"{rel(source)}: {item_kind} {item[identity_field]!r} references legacy manage "
                f"permission keys {legacy_manage}"
            )


def load_contract_schema(path: Path, asset_type: str) -> dict[str, Any]:
    if not path.exists():
        raise ValidationError(f"{path}: contracts {asset_type} schema was requested but does not exist")
    schema = read_json(path)
    if not isinstance(schema, dict):
        raise ValidationError(f"{path}: contracts {asset_type} schema must be a JSON object")
    return schema


def validate_schema(data: Any, schema: dict[str, Any], source: str) -> None:
    _validate_schema_value(data, schema, source, schema)


def _validate_schema_value(data: Any, schema: dict[str, Any], path: str, root_schema: dict[str, Any]) -> None:
    schema = resolve_schema_ref(schema, root_schema, path)
    if "const" in schema and data != schema["const"]:
        raise ValidationError(f"{path}: expected {schema['const']!r}, got {data!r}")
    if "enum" in schema and data not in schema["enum"]:
        raise ValidationError(f"{path}: expected one of {schema['enum']!r}, got {data!r}")

    expected_type = schema.get("type")
    if expected_type:
        if not _type_matches(data, expected_type):
            raise ValidationError(f"{path}: expected {expected_type}, got {type(data).__name__}")

    if isinstance(data, str):
        min_length = schema.get("minLength")
        if min_length is not None and len(data) < min_length:
            raise ValidationError(f"{path}: string is shorter than {min_length}")
        pattern = schema.get("pattern")
        if pattern and re.fullmatch(pattern, data) is None:
            raise ValidationError(f"{path}: value {data!r} does not match {pattern}")

    if isinstance(data, int) and not isinstance(data, bool):
        minimum = schema.get("minimum")
        if minimum is not None and data < minimum:
            raise ValidationError(f"{path}: value must be >= {minimum}")

    if isinstance(data, list):
        min_items = schema.get("minItems")
        if min_items is not None and len(data) < min_items:
            raise ValidationError(f"{path}: array must contain at least {min_items} item(s)")
        if schema.get("uniqueItems"):
            seen = set()
            for index, item in enumerate(data):
                key = json.dumps(item, sort_keys=True)
                if key in seen:
                    raise ValidationError(f"{path}[{index}]: duplicate array item {item!r}")
                seen.add(key)
        item_schema = schema.get("items")
        if item_schema:
            for index, item in enumerate(data):
                _validate_schema_value(item, item_schema, f"{path}[{index}]", root_schema)

    if isinstance(data, dict):
        required = schema.get("required", [])
        for key in required:
            if key not in data:
                raise ValidationError(f"{path}: missing required key {key!r}")
        properties = schema.get("properties", {})
        for key, value in data.items():
            if key in properties:
                _validate_schema_value(value, properties[key], f"{path}.{key}", root_schema)
            elif schema.get("additionalProperties") is False:
                raise ValidationError(f"{path}: unsupported key {key!r}")


def resolve_schema_ref(schema: dict[str, Any], root_schema: dict[str, Any], path: str) -> dict[str, Any]:
    ref = schema.get("$ref")
    if ref is None:
        return schema
    if not isinstance(ref, str) or not ref.startswith("#/"):
        raise ValidationError(f"{path}: unsupported JSON Schema ref {ref!r}")

    target: Any = root_schema
    for raw_token in ref[2:].split("/"):
        token = raw_token.replace("~1", "/").replace("~0", "~")
        if not isinstance(target, dict) or token not in target:
            raise ValidationError(f"{path}: unresolved JSON Schema ref {ref!r}")
        target = target[token]
    if not isinstance(target, dict):
        raise ValidationError(f"{path}: JSON Schema ref {ref!r} does not point to an object")

    merged = dict(target)
    for key, value in schema.items():
        if key != "$ref":
            merged[key] = value
    return merged


def _type_matches(data: Any, expected_type: str) -> bool:
    if expected_type == "object":
        return isinstance(data, dict)
    if expected_type == "array":
        return isinstance(data, list)
    if expected_type == "string":
        return isinstance(data, str)
    if expected_type == "integer":
        return isinstance(data, int) and not isinstance(data, bool)
    if expected_type == "boolean":
        return isinstance(data, bool)
    return True


def validate_contract_schema_alignment(contract_schemas: dict[str, Path | None]) -> dict[str, str]:
    sources: dict[str, str] = {}
    for asset_type, contract_schema in contract_schemas.items():
        if contract_schema is None:
            continue
        local_schema_name = LOCAL_SCHEMA_FILES[asset_type]
        local = load_schema(local_schema_name)
        contract = load_contract_schema(contract_schema, asset_type)
        validate_contract_schema_compatibility(asset_type, local_schema_name, local, contract, contract_schema)
        sources[asset_type] = report_path(contract_schema)
    return sources


def validate_contract_schema_compatibility(
    asset_type: str,
    local_schema_name: str,
    local: dict[str, Any],
    contract: dict[str, Any],
    contract_schema: Path,
) -> None:
    local_properties = set(local.get("properties", {}))
    contract_properties = set(contract.get("properties", {}))
    unsupported_properties = sorted(local_properties - contract_properties)
    if unsupported_properties:
        raise ValidationError(
            f"schemas/{local_schema_name}: official {asset_type} schema exposes properties "
            f"not present in {contract_schema}: {unsupported_properties}"
        )

    for property_name in sorted(local_properties & contract_properties):
        local_property = local["properties"][property_name]
        contract_property = contract["properties"][property_name]
        for key in ("type", "const", "pattern", "additionalProperties"):
            if key in contract_property and key in local_property and local_property[key] != contract_property[key]:
                raise ValidationError(
                    f"schemas/{local_schema_name}: {property_name}.{key} differs from {contract_schema}"
                )
        if "minLength" in contract_property and "minLength" in local_property:
            if local_property["minLength"] < contract_property["minLength"]:
                raise ValidationError(
                    f"schemas/{local_schema_name}: {property_name}.minLength is weaker than {contract_schema}"
                )
        if "enum" in contract_property and "enum" in local_property:
            if not set(local_property["enum"]).issubset(set(contract_property["enum"])):
                raise ValidationError(
                    f"schemas/{local_schema_name}: {property_name}.enum allows values outside {contract_schema}"
                )

    contract_required = set(contract.get("required", []))
    local_required = set(local.get("required", []))
    if not contract_required.issubset(local_required):
        raise ValidationError(
            f"schemas/{local_schema_name}: local required fields must include contracts required fields "
            f"{sorted(contract_required)}"
        )


def extract_front_matter(path: Path) -> tuple[dict[str, Any], str]:
    text = path.read_text()
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValidationError(f"{rel(path)}: missing YAML front matter")
    for index in range(1, len(lines)):
        if lines[index].strip() == "---":
            front_matter = "\n".join(lines[1:index])
            body = "\n".join(lines[index + 1 :])
            parsed = SimpleYAMLParser(front_matter, path).parse()
            if not isinstance(parsed, dict):
                raise ValidationError(f"{rel(path)}: front matter must be a mapping")
            return parsed, body
    raise ValidationError(f"{rel(path)}: unterminated YAML front matter")


def validate_skill_body(path: Path, meta: dict[str, Any], body: str) -> None:
    expected_h1 = f"# {meta['name']}"
    if not re.search(rf"^{re.escape(expected_h1)}$", body, re.MULTILINE):
        raise ValidationError(f"{rel(path)}: missing H1 {expected_h1!r}")
    for section in REQUIRED_SKILL_SECTIONS:
        if not re.search(rf"^## {re.escape(section)}$", body, re.MULTILINE):
            raise ValidationError(f"{rel(path)}: missing required section ## {section}")
    guardrails = section_text(body, "Guardrails")
    if not re.search(r"^- ", guardrails, re.MULTILINE):
        raise ValidationError(f"{rel(path)}: ## Guardrails must contain at least one bullet")
    examples = section_text(body, "Examples")
    if not re.search(r"(?im)^### Input Example$", examples):
        raise ValidationError(f"{rel(path)}: ## Examples must contain ### Input Example")
    if not re.search(r"(?im)^### Expected Tool Calls$", examples):
        raise ValidationError(f"{rel(path)}: ## Examples must contain ### Expected Tool Calls")
    permission_boundaries = section_text(body, "Permission Boundaries")
    if not re.search(r"(?im)^- ", permission_boundaries):
        raise ValidationError(f"{rel(path)}: ## Permission Boundaries must contain at least one bullet")
    forbidden_actions = section_text(body, "Forbidden Actions")
    if not re.search(r"(?im)^- ", forbidden_actions):
        raise ValidationError(f"{rel(path)}: ## Forbidden Actions must contain at least one bullet")
    security_text = "\n".join((guardrails, permission_boundaries, forbidden_actions))
    if not SENSITIVE_TERMS_RE.search(security_text):
        raise ValidationError(f"{rel(path)}: security lint requires explicit sensitive-data handling")
    if SECRET_ASSIGNMENT_RE.search(body):
        raise ValidationError(f"{rel(path)}: security lint found a secret-like assignment")


def section_text(body: str, section: str) -> str:
    match = re.search(rf"^## {re.escape(section)}\n(?P<body>.*?)(?=^## |\Z)", body, re.MULTILINE | re.DOTALL)
    return match.group("body") if match else ""


def validate_skill_capabilities(
    path: Path,
    meta: dict[str, Any],
    catalog_tools: dict[str, dict[str, Any]],
    ai_platform_capabilities: dict[str, dict[str, Any]],
    known_permission_keys: set[str] | None,
) -> None:
    category = meta["category"]
    if category not in ALLOWED_SKILL_CATEGORIES:
        raise ValidationError(f"{rel(path)}: category {category!r} is not in allowed categories {sorted(ALLOWED_SKILL_CATEGORIES)}")

    catalog_scopes: set[str] = set()
    for capability_ref in meta["capabilityRefs"]:
        tool = catalog_tools.get(capability_ref)
        if tool is None:
            raise ValidationError(f"{rel(path)}: unknown capabilityRef {capability_ref!r} in Gateway capability catalog")
        if tool.get("status") != "stable-runtime":
            raise ValidationError(
                f"{rel(path)}: capabilityRef {capability_ref!r} is {tool.get('status')!r}, not stable-runtime"
            )
        catalog_scopes.update(tool["requiredScopes"])

    metadata = meta.get("metadata", {})
    http_refs = metadata.get("httpCapabilityRefs", []) if isinstance(metadata, dict) else []
    if not isinstance(http_refs, list) or any(not isinstance(ref, str) for ref in http_refs):
        raise ValidationError(f"{rel(path)}: metadata.httpCapabilityRefs must be an array of capability keys")
    for capability_ref in http_refs:
        capability = ai_platform_capabilities.get(capability_ref)
        if capability is None:
            raise ValidationError(f"{rel(path)}: unknown HTTP capability ref {capability_ref!r}")
        if capability["status"] == "deprecated":
            raise ValidationError(f"{rel(path)}: HTTP capability ref {capability_ref!r} is deprecated")
        catalog_scopes.update(capability["requiredScopes"])

    unknown_scopes = sorted(set(meta["requiredScopes"]) - catalog_scopes)
    if unknown_scopes:
        raise ValidationError(f"{rel(path)}: requiredScopes are not exposed by referenced Gateway capabilities: {unknown_scopes}")
    if known_permission_keys is not None:
        unknown_permissions = sorted(set(meta.get("permissionKeys", [])) - known_permission_keys)
        if unknown_permissions:
            raise ValidationError(f"{rel(path)}: unknown permission keys {unknown_permissions}")


def extract_gateway_capability_names_from_source(path: Path, function_name: str, type_name: str) -> set[str]:
    if not path.exists():
        raise ValidationError(f"{path}: Gateway catalog source was requested but does not exist")
    text = path.read_text()
    match = re.search(
        rf"func {re.escape(function_name)}\(\).*?return \[\]domainaigateway\.{re.escape(type_name)}\{{(?P<body>.*?)\n\t}}\n}}",
        text,
        re.DOTALL,
    )
    if not match:
        raise ValidationError(f"{path}: could not locate {function_name} catalog")
    return set(re.findall(r'(?m)^\s*Name:\s+"([^"]+)"', match.group("body")))


def extract_gateway_tool_names_from_source(path: Path) -> set[str]:
    text = path.read_text()
    catalogs = re.findall(
        r"var (?:default|operations)ToolCatalog = \[\]domainaigateway\.ToolCapability\{(?P<body>.*?)\n\}",
        text,
        re.DOTALL,
    )
    if catalogs:
        names = set(re.findall(r'(?m)^\s*Name:\s+"([^"]+)"', "\n".join(catalogs)))
    else:
        names = extract_gateway_capability_names_from_source(path, "defaultTools", "ToolCapability")
    for provider_name in ("knowledge_provider.go", "delivery_provider.go", "draft_provider.go", "docker_project_provider.go", "observability_provider.go", "virtualization_provider.go", "resource_creation_provider.go", "conditional_resource_provider.go", "workload_scale_assessment.go", "pod_execution_provider.go"):
        provider = path.with_name(provider_name)
        if provider.exists():
            names.update(re.findall(r'(?:^\s*|\{)Name:\s+"([^"]+)"', provider.read_text(), re.MULTILINE))
    return names


def extract_gateway_resource_names_from_source(path: Path) -> set[str]:
    return extract_gateway_capability_names_from_source(path, "defaultResources", "ResourceCapability")


def extract_gateway_prompt_names_from_source(path: Path) -> set[str]:
    return extract_gateway_capability_names_from_source(path, "defaultPrompts", "PromptCapability")


def extract_platform_capability_keys_from_source(path: Path) -> set[str]:
    if not path.exists():
        raise ValidationError(f"{path}: platform capability source was requested but does not exist")
    text = path.read_text()
    match = re.search(
        r"func DefaultCapabilityMatrix\(\).*?return \[\]CapabilityMatrixEntry\{(?P<body>.*?)\n\t}\n}",
        text,
        re.DOTALL,
    )
    if not match:
        raise ValidationError(f"{path}: could not locate DefaultCapabilityMatrix")
    return set(re.findall(r'\bcapability\("([^"]+)"', match.group("body")))


def validate_gateway_catalog_source_drift(catalog: dict[str, Any], gateway_catalog_source: Path | None) -> None:
    if gateway_catalog_source is None:
        return
    source_names = extract_gateway_tool_names_from_source(gateway_catalog_source)
    catalog_names = {tool["name"] for tool in catalog["tools"] if tool["status"] == "stable-runtime"}
    if source_names != catalog_names:
        raise ValidationError(
            f"{rel(CATALOG_PATH)}: Gateway catalog snapshot differs from {gateway_catalog_source}: "
            f"missing={sorted(source_names - catalog_names)} extra={sorted(catalog_names - source_names)}"
        )

    source_resources = extract_gateway_resource_names_from_source(gateway_catalog_source)
    catalog_resources = {
        resource["name"] for resource in catalog.get("resources", []) if resource["status"] == "stable-runtime"
    }
    if source_resources != catalog_resources:
        raise ValidationError(
            f"{rel(CATALOG_PATH)}: Gateway resource snapshot differs from {gateway_catalog_source}: "
            f"missing={sorted(source_resources - catalog_resources)} extra={sorted(catalog_resources - source_resources)}"
        )

    source_prompts = extract_gateway_prompt_names_from_source(gateway_catalog_source)
    catalog_prompts = {prompt["name"] for prompt in catalog.get("prompts", []) if prompt["status"] == "stable-runtime"}
    if source_prompts != catalog_prompts:
        raise ValidationError(
            f"{rel(CATALOG_PATH)}: Gateway prompt snapshot differs from {gateway_catalog_source}: "
            f"missing={sorted(source_prompts - catalog_prompts)} extra={sorted(catalog_prompts - source_prompts)}"
        )


def validate_platform_catalog_source_drift(catalog: dict[str, Any], platform_capability_source: Path | None) -> None:
    if platform_capability_source is None:
        return
    source_keys = extract_platform_capability_keys_from_source(platform_capability_source)
    catalog_keys = {capability["key"] for capability in catalog["capabilities"]}
    if source_keys != catalog_keys:
        raise ValidationError(
            f"{rel(PLATFORM_CATALOG_PATH)}: platform capability snapshot differs from {platform_capability_source}: "
            f"missing={sorted(source_keys - catalog_keys)} extra={sorted(catalog_keys - source_keys)}"
        )


def validate_skills(
    release_version: str,
    contract_schema_path: Path | None,
    ai_platform_capabilities: dict[str, dict[str, Any]],
    known_permission_keys: set[str] | None,
) -> tuple[dict[str, dict[str, Any]], dict[str, Any]]:
    schema = load_schema("skill-frontmatter.schema.json")
    contract_schema = load_contract_schema(contract_schema_path, "skill") if contract_schema_path is not None else None
    catalog = load_capability_catalog()
    catalog_tools = {tool["name"]: tool for tool in catalog["tools"]}
    skill_paths = sorted((ROOT / "skills").glob("**/SKILL.md"))
    if not skill_paths:
        raise ValidationError("skills: expected at least one SKILL.md")

    skills_by_id: dict[str, dict[str, Any]] = {}
    index_entries: list[dict[str, Any]] = []
    for path in skill_paths:
        meta, body = extract_front_matter(path)
        if contract_schema is not None:
            validate_schema(meta, contract_schema, f"{rel(path)} public contract")
        validate_schema(meta, schema, rel(path))
        skill_id = meta["id"]
        if skill_id in skills_by_id:
            raise ValidationError(f"{rel(path)}: duplicate skill id {skill_id!r}")
        if path.parent.name != skill_id:
            raise ValidationError(f"{rel(path)}: parent directory must match skill id {skill_id!r}")
        validate_skill_capabilities(path, meta, catalog_tools, ai_platform_capabilities, known_permission_keys)
        validate_skill_body(path, meta, body)
        skills_by_id[skill_id] = meta
        index_entries.append(
            {
                "id": skill_id,
                "name": meta["name"],
                "version": meta["version"],
                "category": meta["category"],
                "description": meta["description"],
                "path": rel(path),
                "capabilityRefs": meta["capabilityRefs"],
                "requiredScopes": meta["requiredScopes"],
            }
        )

    for asset_type in ("tools", "resources", "prompts"):
        for asset in catalog.get(asset_type, []):
            for skill_id in asset.get("skillRefs", []):
                if skill_id not in skills_by_id:
                    raise ValidationError(
                        f"{rel(CATALOG_PATH)}: {asset_type[:-1]} {asset['name']!r} references unknown skill {skill_id!r}"
                    )

    index_entries.sort(key=lambda item: item["id"])
    generated_index = {
        "$schema": "../schemas/skills-index.schema.json",
        "schemaVersion": "opensoha.dev/skills-index/v1",
        "version": release_version,
        "skills": index_entries,
    }
    validate_schema(generated_index, load_schema("skills-index.schema.json"), "generated skills index")
    return skills_by_id, generated_index


def validate_agent_skills() -> int:
    skill_paths = sorted(AGENT_SKILLS_ROOT.glob("*/SKILL.md"))
    if not skill_paths:
        raise ValidationError("agent-skills: expected at least one SKILL.md")
    seen: set[str] = set()
    for path in skill_paths:
        meta, body = extract_front_matter(path)
        unknown = sorted(set(meta) - {"name", "description"})
        if unknown:
            raise ValidationError(f"{rel(path)}: unsupported agent skill front matter fields {unknown}")
        name = meta.get("name")
        description = meta.get("description")
        if not isinstance(name, str) or not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name):
            raise ValidationError(f"{rel(path)}: name must be lowercase kebab-case")
        if name in seen:
            raise ValidationError(f"{rel(path)}: duplicate agent skill name {name!r}")
        if path.parent.name != name:
            raise ValidationError(f"{rel(path)}: parent directory must match agent skill name {name!r}")
        if not isinstance(description, str) or not description.strip() or len(description) > 1024:
            raise ValidationError(f"{rel(path)}: description must contain 1-1024 characters")
        if not re.search(rf"^# {re.escape(name.title())}$", body, re.MULTILINE):
            raise ValidationError(f"{rel(path)}: missing H1 '# {name.title()}'")
        if SECRET_ASSIGNMENT_RE.search(body):
            raise ValidationError(f"{rel(path)}: security lint found a secret-like assignment")

        openai_path = path.parent / "agents" / "openai.yaml"
        if not openai_path.is_file():
            raise ValidationError(f"{rel(path)}: missing agents/openai.yaml")
        openai = SimpleYAMLParser(openai_path.read_text(), openai_path).parse()
        interface = openai.get("interface") if isinstance(openai, dict) else None
        if not isinstance(interface, dict):
            raise ValidationError(f"{rel(openai_path)}: interface must be a mapping")
        for field in ("display_name", "short_description", "default_prompt"):
            value = interface.get(field)
            if not isinstance(value, str) or not value.strip():
                raise ValidationError(f"{rel(openai_path)}: interface.{field} must be a non-empty string")
        if f"${name}" not in interface["default_prompt"]:
            raise ValidationError(f"{rel(openai_path)}: interface.default_prompt must reference ${name}")
        seen.add(name)
    return len(skill_paths)


def validate_index(generated_index: dict[str, Any], write_index: bool) -> None:
    index_path = ROOT / "skills" / "index.json"
    if write_index:
        index_path.write_text(json.dumps(generated_index, indent=2) + "\n")
    existing = read_json(index_path)
    validate_schema(existing, load_schema("skills-index.schema.json"), rel(index_path))
    if existing != generated_index:
        raise ValidationError(f"{rel(index_path)}: stale index, run python3 tools/validate_assets.py --write-index")


def required_scope_union(catalog: dict[str, Any]) -> list[str]:
    return sorted(
        {
            scope
            for tool in catalog["tools"]
            if tool["status"] == "stable-runtime"
            for scope in tool["requiredScopes"]
        }
    )


def validate_compatibility_matrix(
    catalog: dict[str, Any], platform_catalog: dict[str, Any], generated_index: dict[str, Any]
) -> dict[str, Any]:
    matrix = read_json(COMPATIBILITY_MATRIX_PATH)
    if not isinstance(matrix, dict):
        raise ValidationError(f"{rel(COMPATIBILITY_MATRIX_PATH)}: compatibility matrix must be a JSON object")
    validate_schema(matrix, load_schema("compatibility-matrix.schema.json"), rel(COMPATIBILITY_MATRIX_PATH))

    if matrix["skillsVersion"] != generated_index["version"]:
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: skillsVersion {matrix['skillsVersion']!r} "
            f"does not match skills/index.json version {generated_index['version']!r}"
        )

    if matrix["gatewayCapabilityCatalogVersion"] != catalog["version"]:
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: gatewayCapabilityCatalogVersion "
            f"{matrix['gatewayCapabilityCatalogVersion']!r} does not match catalog version {catalog['version']!r}"
        )

    if matrix["platformCapabilityCatalogVersion"] != platform_catalog["version"]:
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: platformCapabilityCatalogVersion "
            f"{matrix['platformCapabilityCatalogVersion']!r} does not match platform catalog version {platform_catalog['version']!r}"
        )

    expected_scopes = required_scope_union(catalog)
    if matrix["requiredScopes"] != expected_scopes:
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: requiredScopes must equal the normalized Gateway catalog union: "
            f"expected={expected_scopes} actual={matrix['requiredScopes']}"
        )

    supported_versions = matrix["supportedVersions"]
    for component in ("soha-core", "soha-cli", "soha-agent"):
        if not supported_versions.get(component):
            raise ValidationError(f"{rel(COMPATIBILITY_MATRIX_PATH)}: supportedVersions.{component} is required")

    contract_schemas = matrix["contractSchemas"]
    if contract_schemas["preferred"] != "../contracts":
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: contractSchemas.preferred must point at ../contracts"
        )
    if contract_schemas["fallback"] != "../node_modules/@opensoha/contracts":
        raise ValidationError(
            f"{rel(COMPATIBILITY_MATRIX_PATH)}: contractSchemas.fallback must point at ../node_modules/@opensoha/contracts"
        )
    assets = contract_schemas["assets"]
    expected_assets = {
        "skillManifest": CONTRACT_SCHEMA_RELATIVE_PATHS["skill"],
        "mcpPreset": CONTRACT_SCHEMA_RELATIVE_PATHS["mcpPreset"],
        "agentProfile": CONTRACT_SCHEMA_RELATIVE_PATHS["agentProfile"],
    }
    for asset_name, relative_path in expected_assets.items():
        asset = assets[asset_name]
        expected_preferred = f"../contracts/{relative_path}"
        expected_fallback = f"../node_modules/@opensoha/contracts/{relative_path}"
        if asset["preferred"] != expected_preferred:
            raise ValidationError(
                f"{rel(COMPATIBILITY_MATRIX_PATH)}: contractSchemas.assets.{asset_name}.preferred "
                f"must be {expected_preferred!r}"
            )
        if asset["fallback"] != expected_fallback:
            raise ValidationError(
                f"{rel(COMPATIBILITY_MATRIX_PATH)}: contractSchemas.assets.{asset_name}.fallback "
                f"must be {expected_fallback!r}"
            )

    return matrix


def validate_catalog_readme(matrix: dict[str, Any]) -> None:
    text = CATALOG_README_PATH.read_text()
    required = [
        f"Skills package version: `{matrix['skillsVersion']}`",
        f"Gateway capability catalog version: `{matrix['gatewayCapabilityCatalogVersion']}`",
        f"Platform capability catalog version: `{matrix['platformCapabilityCatalogVersion']}`",
        f"Supported `soha-core`: `{matrix['supportedVersions']['soha-core']}`",
        f"Supported `soha-cli`: `{matrix['supportedVersions']['soha-cli']}`",
        f"Supported `soha-agent`: `{matrix['supportedVersions']['soha-agent']}`",
        f"`{matrix['contractSchemas']['preferred']}`",
        f"`{matrix['contractSchemas']['fallback']}`",
        "asset-governance.json",
        "install audit",
        "`~/.soha/skills`",
        "Roll back",
    ]
    for asset in matrix["contractSchemas"]["assets"].values():
        required.append(f"`{asset['preferred']}`")
        required.append(f"`{asset['fallback']}`")
    required.extend(f"- `{scope}`" for scope in matrix["requiredScopes"])
    require_text_includes(rel(CATALOG_README_PATH), text, required)


def require_text_includes(name: str, text: str, required: list[str]) -> None:
    missing = [item for item in required if item not in text]
    if missing:
        raise ValidationError(f"{name}: missing required content {missing}")


def platform_scope_union(platform_capabilities: dict[str, dict[str, Any]], capability_refs: list[str], source: Path) -> set[str]:
    scopes: set[str] = set()
    for capability_ref in capability_refs:
        capability = platform_capabilities.get(capability_ref)
        if capability is None:
            raise ValidationError(f"{rel(source)}: unknown platformCapabilityRef {capability_ref!r}")
        scopes.update(capability["requiredScopes"])
    return scopes


def validate_mcp_presets(
    skills_by_id: dict[str, dict[str, Any]],
    gateway_tools: dict[str, dict[str, Any]],
    platform_capabilities: dict[str, dict[str, Any]],
    contract_schema_path: Path | None,
) -> dict[str, dict[str, Any]]:
    schema = load_schema("mcp-preset.schema.json")
    contract_schema = load_contract_schema(contract_schema_path, "mcpPreset") if contract_schema_path is not None else None
    preset_paths = sorted((ROOT / "mcp-presets").glob("*.yaml"))
    if not preset_paths:
        raise ValidationError("mcp-presets: expected at least one MCP preset YAML file")
    presets_by_id: dict[str, dict[str, Any]] = {}
    for path in preset_paths:
        preset = read_yaml(path)
        if contract_schema is not None:
            validate_schema(preset, contract_schema, f"{rel(path)} public contract")
        validate_schema(preset, schema, rel(path))
        preset_id = preset["id"]
        if preset_id in presets_by_id:
            raise ValidationError(f"{rel(path)}: duplicate MCP preset id {preset_id!r}")
        presets_by_id[preset_id] = preset
        platform_scopes = platform_scope_union(platform_capabilities, preset["platformCapabilityRefs"], path)
        unknown_scopes = sorted(set(preset["requiredScopes"]) - platform_scopes)
        if unknown_scopes:
            raise ValidationError(
                f"{rel(path)}: requiredScopes are not exposed by referenced platformCapabilityRefs: {unknown_scopes}"
            )
        tool_names: set[str] = set()
        for tool in preset["tools"]:
            name = tool["name"]
            catalog_tool = gateway_tools.get(name)
            if catalog_tool is None:
                raise ValidationError(f"{rel(path)}: unknown Gateway tool {name!r}")
            if catalog_tool["status"] != "stable-runtime":
                raise ValidationError(f"{rel(path)}: Gateway tool {name!r} is not stable-runtime")
            if tool["riskLevel"] != catalog_tool["riskLevel"]:
                raise ValidationError(f"{rel(path)}: Gateway tool {name!r} riskLevel does not match catalog")
            if set(tool["requiredScopes"]) != set(catalog_tool["requiredScopes"]):
                raise ValidationError(f"{rel(path)}: Gateway tool {name!r} requiredScopes do not match catalog")
            tool_names.add(name)
        for skill_id in preset["skillRefs"]:
            skill = skills_by_id.get(skill_id)
            if skill is None:
                raise ValidationError(f"{rel(path)}: unknown skillRef {skill_id!r}")
            missing = sorted(set(skill["capabilityRefs"]) - tool_names)
            if missing:
                raise ValidationError(f"{rel(path)}: preset does not cover {skill_id!r} capabilityRefs: {missing}")
    return presets_by_id


def validate_agent_profiles(
    skills_by_id: dict[str, dict[str, Any]],
    presets_by_id: dict[str, dict[str, Any]],
    platform_capabilities: dict[str, dict[str, Any]],
    contract_schema_path: Path | None,
) -> dict[str, dict[str, Any]]:
    schema = load_schema("agent-profile.schema.json")
    contract_schema = (
        load_contract_schema(contract_schema_path, "agentProfile") if contract_schema_path is not None else None
    )
    profile_paths = sorted((ROOT / "agent-profiles").glob("*.yaml"))
    if not profile_paths:
        raise ValidationError("agent-profiles: expected at least one agent profile YAML file")
    profiles_by_id: dict[str, dict[str, Any]] = {}
    for path in profile_paths:
        profile = read_yaml(path)
        if contract_schema is not None:
            validate_schema(profile, contract_schema, f"{rel(path)} public contract")
        validate_schema(profile, schema, rel(path))
        profile_id = profile["id"]
        if profile_id in profiles_by_id:
            raise ValidationError(f"{rel(path)}: duplicate agent profile id {profile_id!r}")
        profiles_by_id[profile_id] = profile

        profile_skills = set(profile["skillRefs"])
        for skill_id in profile_skills:
            if skill_id not in skills_by_id:
                raise ValidationError(f"{rel(path)}: unknown skillRef {skill_id!r}")
        profile_platform_scopes = platform_scope_union(platform_capabilities, profile["platformCapabilityRefs"], path)
        unknown_profile_scopes = sorted(set(profile["requiredScopes"]) - profile_platform_scopes)
        if unknown_profile_scopes:
            raise ValidationError(
                f"{rel(path)}: requiredScopes are not exposed by referenced platformCapabilityRefs: {unknown_profile_scopes}"
            )

        preset_tools: set[str] = set()
        preset_skills: set[str] = set()
        preset_platform_refs: set[str] = set()
        preset_scopes: set[str] = set()
        for preset_id in profile["mcpPresetRefs"]:
            preset = presets_by_id.get(preset_id)
            if preset is None:
                raise ValidationError(f"{rel(path)}: unknown mcpPresetRef {preset_id!r}")
            preset_tools.update(tool["name"] for tool in preset["tools"])
            preset_skills.update(preset["skillRefs"])
            preset_platform_refs.update(preset["platformCapabilityRefs"])
            preset_scopes.update(preset["requiredScopes"])

        missing_skills = sorted(profile_skills - preset_skills)
        if missing_skills:
            raise ValidationError(f"{rel(path)}: profile skills are not present in referenced presets: {missing_skills}")
        missing_tools = sorted(set(profile["enabledToolRefs"]) - preset_tools)
        if missing_tools:
            raise ValidationError(f"{rel(path)}: enabledToolRefs are not present in referenced presets: {missing_tools}")
        missing_platform_refs = sorted(set(profile["platformCapabilityRefs"]) - preset_platform_refs)
        if missing_platform_refs:
            raise ValidationError(
                f"{rel(path)}: platformCapabilityRefs are not present in referenced presets: {missing_platform_refs}"
            )
        missing_scopes = sorted(set(profile["requiredScopes"]) - preset_scopes)
        if missing_scopes:
            raise ValidationError(f"{rel(path)}: requiredScopes are not present in referenced presets: {missing_scopes}")
    return profiles_by_id


RISK_LEVEL_ORDER = {"read": 0, "analyze": 1, "mutate": 2, "execute": 3, "high": 4}


def validate_asset_governance(
    skills_by_id: dict[str, dict[str, Any]],
    presets_by_id: dict[str, dict[str, Any]],
    profiles_by_id: dict[str, dict[str, Any]],
    gateway_tools: dict[str, dict[str, Any]],
    platform_capabilities: dict[str, dict[str, Any]],
    ai_platform_capabilities: dict[str, dict[str, Any]],
) -> dict[str, int]:
    governance = read_json(ASSET_GOVERNANCE_PATH)
    if not isinstance(governance, dict):
        raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: governance catalog must be a JSON object")
    validate_schema(governance, load_schema("asset-governance.schema.json"), rel(ASSET_GOVERNANCE_PATH))

    validate_release_signing_policy(governance["releaseSigning"])
    validate_install_audit_policy(governance["installAudit"])

    expected_reviews = expected_asset_reviews(
        skills_by_id, presets_by_id, profiles_by_id, gateway_tools, platform_capabilities, ai_platform_capabilities
    )
    actual_reviews: dict[tuple[str, str], dict[str, Any]] = {}
    for review in governance["permissionReview"]["assets"]:
        key = (review["type"], review["id"])
        if key in actual_reviews:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: duplicate permission review for {key[0]} {key[1]!r}")
        actual_reviews[key] = review

    missing = sorted(set(expected_reviews) - set(actual_reviews))
    extra = sorted(set(actual_reviews) - set(expected_reviews))
    if missing or extra:
        raise ValidationError(
            f"{rel(ASSET_GOVERNANCE_PATH)}: permission review coverage mismatch missing={missing} extra={extra}"
        )

    for key, expected in expected_reviews.items():
        review = actual_reviews[key]
        for field in ("version", "riskLevel", "approvalRequired"):
            if review[field] != expected[field]:
                raise ValidationError(
                    f"{rel(ASSET_GOVERNANCE_PATH)}: {key[0]} {key[1]!r} {field} "
                    f"must be {expected[field]!r}, got {review[field]!r}"
                )
        for field in ("gatewayCapabilityRefs", "httpCapabilityRefs", "platformCapabilityRefs", "requiredScopes"):
            if sorted(review.get(field, [])) != sorted(expected.get(field, [])):
                raise ValidationError(
                    f"{rel(ASSET_GOVERNANCE_PATH)}: {key[0]} {key[1]!r} {field} "
                    f"must be {sorted(expected.get(field, []))}, got {sorted(review.get(field, []))}"
                )
        if review["reviewStatus"] != "approved":
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: {key[0]} {key[1]!r} is not approved")

    return {
        "permissionReviews": len(actual_reviews),
        "signedArtifacts": len(governance["releaseSigning"]["signedArtifacts"]),
        "installAuditEvents": len(governance["installAudit"]["requiredEvents"]),
    }


def validate_release_signing_policy(policy: dict[str, Any]) -> None:
    required = {"releaseTarball", "checksum", "manifest", "validationReport", "githubBuildProvenance"}
    signed_artifacts = set(policy["signedArtifacts"])
    missing = sorted(required - signed_artifacts)
    if missing:
        raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: releaseSigning.signedArtifacts missing {missing}")
    if not policy["provenanceRequired"]:
        raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: releaseSigning.provenanceRequired must be true")


def validate_install_audit_policy(policy: dict[str, Any]) -> None:
    event_schema_path = ROOT / policy["eventSchema"]
    if not event_schema_path.exists():
        raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: installAudit.eventSchema {policy['eventSchema']!r} not found")
    event_schema = read_json(event_schema_path)
    if not isinstance(event_schema, dict):
        raise ValidationError(f"{rel(event_schema_path)}: install audit event schema must be a JSON object")
    event_type = event_schema.get("properties", {}).get("eventType", {})
    supported_events = set(event_type.get("enum", []))
    missing = sorted(set(policy["requiredEvents"]) - supported_events)
    if missing:
        raise ValidationError(f"{rel(event_schema_path)}: eventType enum is missing audit events {missing}")
    required_fields = set(event_schema.get("required", []))
    for field in ("eventId", "eventType", "occurredAt", "assetType", "assetId", "assetVersion", "packageVersion", "actor", "source", "checksum", "decision"):
        if field not in required_fields:
            raise ValidationError(f"{rel(event_schema_path)}: missing required audit field {field!r}")


def expected_asset_reviews(
    skills_by_id: dict[str, dict[str, Any]],
    presets_by_id: dict[str, dict[str, Any]],
    profiles_by_id: dict[str, dict[str, Any]],
    gateway_tools: dict[str, dict[str, Any]],
    platform_capabilities: dict[str, dict[str, Any]],
    ai_platform_capabilities: dict[str, dict[str, Any]],
) -> dict[tuple[str, str], dict[str, Any]]:
    expected: dict[tuple[str, str], dict[str, Any]] = {}
    for skill_id, skill in skills_by_id.items():
        gateway_refs = list(skill["capabilityRefs"])
        metadata = skill.get("metadata", {})
        http_refs = list(metadata.get("httpCapabilityRefs", [])) if isinstance(metadata, dict) else []
        expected[("skill", skill_id)] = {
            "version": skill["version"],
            "gatewayCapabilityRefs": gateway_refs,
            "httpCapabilityRefs": http_refs,
            "platformCapabilityRefs": [],
            "requiredScopes": skill["requiredScopes"],
            "riskLevel": highest_risk(
                [highest_gateway_risk(gateway_refs, gateway_tools), highest_http_risk(http_refs, ai_platform_capabilities)]
            ),
            "approvalRequired": gateway_approval_required(gateway_refs, gateway_tools)
            or http_approval_required(http_refs, ai_platform_capabilities),
        }
    for preset_id, preset in presets_by_id.items():
        gateway_refs = [tool["name"] for tool in preset["tools"]]
        platform_refs = list(preset["platformCapabilityRefs"])
        expected[("mcpPreset", preset_id)] = {
            "version": preset["version"],
            "gatewayCapabilityRefs": gateway_refs,
            "httpCapabilityRefs": [],
            "platformCapabilityRefs": platform_refs,
            "requiredScopes": preset["requiredScopes"],
            "riskLevel": highest_risk(
                [preset["riskLevel"], highest_gateway_risk(gateway_refs, gateway_tools), highest_platform_risk(platform_refs, platform_capabilities)]
            ),
            "approvalRequired": gateway_approval_required(gateway_refs, gateway_tools)
            or platform_approval_required(platform_refs, platform_capabilities),
        }
    for profile_id, profile in profiles_by_id.items():
        gateway_refs = list(profile["enabledToolRefs"])
        platform_refs = list(profile["platformCapabilityRefs"])
        expected[("agentProfile", profile_id)] = {
            "version": profile["version"],
            "gatewayCapabilityRefs": gateway_refs,
            "httpCapabilityRefs": [],
            "platformCapabilityRefs": platform_refs,
            "requiredScopes": profile["requiredScopes"],
            "riskLevel": highest_risk(
                [highest_gateway_risk(gateway_refs, gateway_tools), highest_platform_risk(platform_refs, platform_capabilities)]
            ),
            "approvalRequired": gateway_approval_required(gateway_refs, gateway_tools)
            or platform_approval_required(platform_refs, platform_capabilities),
        }
    return expected


def highest_gateway_risk(refs: list[str], gateway_tools: dict[str, dict[str, Any]]) -> str:
    levels = []
    for ref in refs:
        tool = gateway_tools.get(ref)
        if tool is None:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown gateway capability ref {ref!r}")
        levels.append(tool["riskLevel"])
    return highest_risk(levels)


def highest_platform_risk(refs: list[str], platform_capabilities: dict[str, dict[str, Any]]) -> str:
    levels = []
    for ref in refs:
        capability = platform_capabilities.get(ref)
        if capability is None:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown platform capability ref {ref!r}")
        levels.append(capability["riskLevel"])
    return highest_risk(levels)


def highest_http_risk(refs: list[str], capabilities: dict[str, dict[str, Any]]) -> str:
    levels = []
    for ref in refs:
        capability = capabilities.get(ref)
        if capability is None:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown HTTP capability ref {ref!r}")
        levels.append(capability["riskLevel"])
    return highest_risk(levels)


def highest_risk(levels: list[str]) -> str:
    if not levels:
        return "read"
    for level in levels:
        if level not in RISK_LEVEL_ORDER:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown risk level {level!r}")
    return max(levels, key=lambda level: RISK_LEVEL_ORDER[level])


def gateway_approval_required(refs: list[str], gateway_tools: dict[str, dict[str, Any]]) -> bool:
    return RISK_LEVEL_ORDER[highest_gateway_risk(refs, gateway_tools)] >= RISK_LEVEL_ORDER["execute"]


def platform_approval_required(refs: list[str], platform_capabilities: dict[str, dict[str, Any]]) -> bool:
    for ref in refs:
        capability = platform_capabilities.get(ref)
        if capability is None:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown platform capability ref {ref!r}")
        if capability["requiresApproval"]:
            return True
    return False


def http_approval_required(refs: list[str], capabilities: dict[str, dict[str, Any]]) -> bool:
    for ref in refs:
        capability = capabilities.get(ref)
        if capability is None:
            raise ValidationError(f"{rel(ASSET_GOVERNANCE_PATH)}: unknown HTTP capability ref {ref!r}")
        if capability["requiresApproval"]:
            return True
    return False


def validate_schema_files() -> None:
    for path in sorted(SCHEMA_DIR.glob("*.schema.json")):
        schema = read_json(path)
        if not isinstance(schema, dict):
            raise ValidationError(f"{rel(path)}: schema must be an object")
        for key in ("$schema", "$id", "title", "type"):
            if key not in schema:
                raise ValidationError(f"{rel(path)}: missing schema key {key!r}")


def release_manifest(version: str) -> dict[str, Any]:
    files: list[dict[str, Any]] = []
    for root_dir in RELEASE_INCLUDE_DIRS:
        for path in sorted((ROOT / root_dir).glob("**/*")):
            if path.is_file():
                files.append(release_file_entry(path))
    for file_name in RELEASE_INCLUDE_FILES:
        path = ROOT / file_name
        if path.exists():
            files.append(release_file_entry(path))
    files.sort(key=lambda item: item["path"])
    manifest = {
        "schemaVersion": "opensoha.dev/skills-release/v1",
        "version": version,
        "format": "tar.gz",
        "artifact": f"soha-skills-{version}.tar.gz",
        "manifestUrl": f"https://github.com/opensoha/soha/releases/download/skills%2Fv{version}/soha-skills-{version}.manifest.json",
        "files": files,
    }
    validate_schema(manifest, load_schema("skills-release-manifest.schema.json"), "generated release manifest")
    return manifest


def release_file_entry(path: Path) -> dict[str, str]:
    return {"path": rel(path), "sha256": sha256_file(path)}


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def package_release(version: str, output_dir: Path) -> dict[str, Any]:
    manifest = release_manifest(version)
    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / f"soha-skills-{version}.manifest.json"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
    artifact_path = output_dir / manifest["artifact"]
    with artifact_path.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for item in manifest["files"]:
                    add_file_to_release_archive(archive, ROOT / item["path"], f"soha-skills/{item['path']}")
                add_file_to_release_archive(archive, manifest_path, f"soha-skills/{manifest_path.name}")
    checksum = sha256_file(artifact_path)
    checksum_path = output_dir / f"{manifest['artifact']}.sha256"
    checksum_path.write_text(f"{checksum}  {manifest['artifact']}\n")
    verification = verify_release_package(artifact_path, version)
    return {
        "artifactPath": artifact_path,
        "manifestPath": manifest_path,
        "checksumPath": checksum_path,
        "sha256": verification["sha256"],
        "manifestSha256": verification["manifestSha256"],
        "checksumFileSha256": verification["checksumFileSha256"],
        "files": verification["files"],
        "members": verification["members"],
    }


def verify_release_package(artifact_path: Path, expected_version: str) -> dict[str, Any]:
    if not artifact_path.exists():
        raise ValidationError(f"{artifact_path}: release package does not exist")

    checksum_path = artifact_path.with_name(f"{artifact_path.name}.sha256")
    manifest_path = artifact_path.with_name(artifact_path.name.removesuffix(".tar.gz") + ".manifest.json")
    if not checksum_path.exists():
        raise ValidationError(f"{checksum_path}: release checksum file does not exist")
    if not manifest_path.exists():
        raise ValidationError(f"{manifest_path}: release manifest file does not exist")

    checksum_text = checksum_path.read_text().strip()
    checksum_match = re.fullmatch(r"([a-f0-9]{64})\s+\*?(.+)", checksum_text)
    if checksum_match is None:
        raise ValidationError(f"{checksum_path}: expected '<sha256>  <artifact>'")
    expected_sha256, checksum_artifact_name = checksum_match.groups()
    if checksum_artifact_name != artifact_path.name:
        raise ValidationError(
            f"{checksum_path}: checksum file names {checksum_artifact_name!r}, expected {artifact_path.name!r}"
        )

    actual_sha256 = sha256_file(artifact_path)
    if actual_sha256 != expected_sha256:
        raise ValidationError(f"{artifact_path}: sha256 mismatch, expected {expected_sha256}, got {actual_sha256}")

    manifest = read_json(manifest_path)
    if not isinstance(manifest, dict):
        raise ValidationError(f"{manifest_path}: release manifest must be a JSON object")
    validate_schema(manifest, load_schema("skills-release-manifest.schema.json"), rel(manifest_path))
    if manifest["version"] != expected_version:
        raise ValidationError(
            f"{manifest_path}: manifest version {manifest['version']!r} does not match release version {expected_version!r}"
        )
    if manifest["artifact"] != artifact_path.name:
        raise ValidationError(
            f"{manifest_path}: manifest artifact {manifest['artifact']!r} does not match {artifact_path.name!r}"
        )

    embedded_manifest_name = f"soha-skills/{manifest_path.name}"
    expected_members = {embedded_manifest_name}
    expected_members.update(f"soha-skills/{item['path']}" for item in manifest["files"])

    with tarfile.open(artifact_path, "r:gz") as archive:
        members = {member.name: member for member in archive.getmembers()}
        member_names = set(members)
        missing_members = sorted(expected_members - member_names)
        extra_members = sorted(member_names - expected_members)
        if missing_members:
            raise ValidationError(f"{artifact_path}: release package is missing members {missing_members}")
        if extra_members:
            raise ValidationError(f"{artifact_path}: release package has unexpected members {extra_members}")

        embedded_manifest = read_tar_member(archive, embedded_manifest_name, artifact_path)
        external_manifest = manifest_path.read_bytes()
        if embedded_manifest != external_manifest:
            raise ValidationError(f"{artifact_path}: embedded release manifest differs from {manifest_path}")

        for item in manifest["files"]:
            member_name = f"soha-skills/{item['path']}"
            member_bytes = read_tar_member(archive, member_name, artifact_path)
            actual_member_sha256 = hashlib.sha256(member_bytes).hexdigest()
            if actual_member_sha256 != item["sha256"]:
                raise ValidationError(
                    f"{artifact_path}: member {member_name} sha256 mismatch, "
                    f"expected {item['sha256']}, got {actual_member_sha256}"
                )

        index_bytes = read_tar_member(archive, "soha-skills/skills/index.json", artifact_path)
        index = json.loads(index_bytes)
        if not isinstance(index, dict):
            raise ValidationError(f"{artifact_path}: embedded skills/index.json must be a JSON object")
        validate_schema(index, load_schema("skills-index.schema.json"), "embedded skills/index.json")
        if index["version"] != manifest["version"]:
            raise ValidationError(
                f"{artifact_path}: embedded skills/index.json version {index['version']!r} "
                f"does not match manifest version {manifest['version']!r}"
            )

    return {
        "artifact": report_path(artifact_path),
        "checksumFile": report_path(checksum_path),
        "manifest": report_path(manifest_path),
        "sha256": actual_sha256,
        "manifestSha256": sha256_file(manifest_path),
        "checksumFileSha256": sha256_file(checksum_path),
        "files": len(manifest["files"]),
        "members": len(expected_members),
    }


def read_tar_member(archive: tarfile.TarFile, member_name: str, artifact_path: Path) -> bytes:
    member = archive.getmember(member_name)
    if not member.isfile():
        raise ValidationError(f"{artifact_path}: member {member_name} is not a regular file")
    extracted = archive.extractfile(member)
    if extracted is None:
        raise ValidationError(f"{artifact_path}: could not read member {member_name}")
    return extracted.read()


def add_file_to_release_archive(archive: tarfile.TarFile, path: Path, arcname: str) -> None:
    info = archive.gettarinfo(path, arcname=arcname)
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    info.mtime = 0
    info.mode = 0o644
    with path.open("rb") as handle:
        archive.addfile(info, handle)


def package_release_dry_run(version: str) -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="soha-skills-package-") as tmp:
        package = package_release(version, Path(tmp))
        return {
            "artifact": package["artifactPath"].name,
            "sha256": package["sha256"],
            "manifestSha256": package["manifestSha256"],
            "checksumFileSha256": package["checksumFileSha256"],
            "files": package["files"],
            "members": package["members"],
        }


def check(name: str, status: str = "passed", **details: Any) -> dict[str, Any]:
    item: dict[str, Any] = {"name": name, "status": status}
    if details:
        item["details"] = details
    return item


def utc_now() -> str:
    epoch = os.environ.get("SOURCE_DATE_EPOCH")
    if epoch is None:
        value = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
    else:
        if not re.fullmatch(r"[0-9]+", epoch):
            raise ValidationError("SOURCE_DATE_EPOCH must be a non-negative integer")
        try:
            value = datetime.datetime.fromtimestamp(int(epoch), datetime.timezone.utc)
        except (ValueError, OverflowError, OSError) as exc:
            raise ValidationError("SOURCE_DATE_EPOCH is outside the supported timestamp range") from exc
    return value.isoformat().replace("+00:00", "Z")


def report_path(path: Path | None) -> str:
    if path is None:
        return ""
    if "SOURCE_DATE_EPOCH" in os.environ and path.is_absolute():
        return Path(os.path.relpath(path, ROOT)).as_posix()
    return rel(path)


def build_validation_report(
    status: str,
    release_version: str,
    checks: list[dict[str, Any]],
    summary: dict[str, Any],
    error: str = "",
) -> dict[str, Any]:
    report = {
        "schemaVersion": "opensoha.dev/skills-validation-report/v1",
        "repository": "soha-skills",
        "generatedAt": utc_now(),
        "releaseVersion": release_version,
        "status": status,
        "checks": checks,
        "summary": summary,
        "error": error,
    }
    validate_schema(report, load_schema("skills-validation-report.schema.json"), "validation report")
    return report


def write_validation_report(path: Path | None, report: dict[str, Any]) -> None:
    if path is None:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")


def run_validation(args: argparse.Namespace) -> tuple[int, dict[str, Any], dict[str, Any]]:
    utc_now()  # Reject malformed reproducible-build input before writing packages or indexes.
    checks: list[dict[str, Any]] = []
    contract_schema_paths = {
        "skill": args.contracts_skill_schema,
        "mcpPreset": args.contracts_mcp_preset_schema,
        "agentProfile": args.contracts_agent_profile_schema,
    }
    summary: dict[str, Any] = {
        "contractsSkillSchema": report_path(args.contracts_skill_schema),
        "contractSchemas": {asset_type: report_path(path) for asset_type, path in contract_schema_paths.items()},
        "gatewayCatalogSource": report_path(args.gateway_catalog_source),
        "platformCapabilitySource": report_path(args.platform_capability_source),
        "contractsPermissionCatalog": report_path(args.contracts_permission_catalog),
        "writeIndex": args.write_index,
    }
    context: dict[str, Any] = {
        "skills_by_id": {},
        "presets_by_id": {},
        "profiles_by_id": {},
        "agent_profile_count": 0,
        "package_summary": None,
        "package_output_summary": None,
        "package_verify_summary": None,
    }
    current_check = ""

    try:
        current_check = "same-source-inputs"
        validate_source_inputs(args)
        checks.append(check(current_check, sources={name: report_path(path) for name, path in required_source_inputs().items()}))

        current_check = "schema-files"
        validate_schema_files()
        schema_count = len(list(SCHEMA_DIR.glob("*.schema.json")))
        summary["schemas"] = schema_count
        checks.append(check(current_check, count=schema_count))

        current_check = "contracts-schema-alignment"
        contract_sources = validate_contract_schema_alignment(contract_schema_paths)
        if not contract_sources:
            checks.append(check(current_check, "skipped", reason="soha-contracts schemas were not provided"))
        else:
            checks.append(check(current_check, sources=contract_sources))

        current_check = "gateway-capability-catalog"
        catalog = load_capability_catalog()
        gateway_tools = {tool["name"]: tool for tool in catalog["tools"]}
        gateway_status_counts: dict[str, int] = {}
        for tool in catalog["tools"]:
            gateway_status_counts[tool["status"]] = gateway_status_counts.get(tool["status"], 0) + 1
        summary["gatewayCatalogVersion"] = catalog["version"]
        summary["gatewayTools"] = len(catalog["tools"])
        summary["gatewayRuntimeTools"] = len(catalog["_runtimeToolNames"])
        summary["gatewayToolStatusCounts"] = gateway_status_counts
        ai_platform_catalog = load_ai_platform_capability_catalog()
        checks.append(
            check(
                current_check,
                version=catalog["version"],
                tools=len(catalog["tools"]),
                runtimeTools=len(catalog["_runtimeToolNames"]),
                statuses=gateway_status_counts,
            )
        )

        current_check = "permission-catalog-conformance"
        known_permission_keys: set[str] | None = None
        if args.contracts_permission_catalog is None:
            checks.append(check(current_check, "skipped", reason="soha-contracts permission catalog was not provided"))
        else:
            permission_catalog, known_permission_keys = load_permission_catalog(args.contracts_permission_catalog)
            for section in ("tools", "resources", "prompts"):
                validate_catalog_permission_keys(
                    CATALOG_PATH,
                    f"Gateway {section[:-1]}",
                    catalog.get(section, []),
                    "name",
                    permission_catalog,
                    known_permission_keys,
                )
            validate_catalog_permission_keys(
                AI_PLATFORM_CATALOG_PATH,
                "AI platform capability",
                ai_platform_catalog["capabilities"],
                "key",
                permission_catalog,
                known_permission_keys,
            )
            summary["permissionCatalogVersion"] = permission_catalog.get("catalogVersion", "")
            summary["permissionCatalogHash"] = permission_catalog.get("contentHash", "")
            summary["permissionKeys"] = len(known_permission_keys)
            checks.append(
                check(
                    current_check,
                    source=report_path(args.contracts_permission_catalog),
                    version=permission_catalog.get("catalogVersion", ""),
                    contentHash=permission_catalog.get("contentHash", ""),
                    permissionKeys=len(known_permission_keys),
                )
            )

        current_check = "gateway-catalog-source-drift"
        if args.gateway_catalog_source is None:
            checks.append(check(current_check, "skipped", reason="Soha Gateway catalog source was not provided"))
        else:
            validate_gateway_catalog_source_drift(catalog, args.gateway_catalog_source)
            checks.append(check(current_check, source=report_path(args.gateway_catalog_source)))

        current_check = "platform-capability-catalog"
        platform_catalog = load_platform_capability_catalog()
        platform_capabilities = {capability["key"]: capability for capability in platform_catalog["capabilities"]}
        summary["platformCatalogVersion"] = platform_catalog["version"]
        summary["platformCapabilities"] = len(platform_catalog["capabilities"])
        checks.append(
            check(
                current_check,
                version=platform_catalog["version"],
                capabilities=len(platform_catalog["capabilities"]),
            )
        )

        current_check = "platform-capability-source-drift"
        if args.platform_capability_source is None:
            checks.append(check(current_check, "skipped", reason="Soha platform capability source was not provided"))
        else:
            validate_platform_catalog_source_drift(platform_catalog, args.platform_capability_source)
            checks.append(check(current_check, source=report_path(args.platform_capability_source)))

        current_check = "ai-platform-capability-catalog"
        ai_platform_capabilities = {capability["key"]: capability for capability in ai_platform_catalog["capabilities"]}
        summary["aiPlatformCapabilities"] = len(ai_platform_capabilities)
        checks.append(
            check(current_check, version=ai_platform_catalog["version"], capabilities=len(ai_platform_capabilities))
        )

        current_check = "skills"
        skills_by_id, generated_index = validate_skills(
            args.release_version,
            args.contracts_skill_schema,
            ai_platform_capabilities,
            known_permission_keys,
        )
        context["skills_by_id"] = skills_by_id
        summary["skills"] = len(skills_by_id)
        checks.append(check(current_check, count=len(skills_by_id)))

        current_check = "agent-skills"
        agent_skill_count = validate_agent_skills()
        summary["agentSkills"] = agent_skill_count
        checks.append(check(current_check, count=agent_skill_count))

        current_check = "skills-index"
        validate_index(generated_index, args.write_index)
        checks.append(check(current_check, path="skills/index.json", writeIndex=args.write_index))

        current_check = "compatibility-matrix"
        compatibility_matrix = validate_compatibility_matrix(catalog, platform_catalog, generated_index)
        summary["compatibilityMatrix"] = {
            "skillsVersion": compatibility_matrix["skillsVersion"],
            "gatewayCapabilityCatalogVersion": compatibility_matrix["gatewayCapabilityCatalogVersion"],
            "platformCapabilityCatalogVersion": compatibility_matrix["platformCapabilityCatalogVersion"],
            "requiredScopes": len(compatibility_matrix["requiredScopes"]),
            "supportedVersions": compatibility_matrix["supportedVersions"],
        }
        checks.append(
            check(
                current_check,
                path=rel(COMPATIBILITY_MATRIX_PATH),
                requiredScopes=len(compatibility_matrix["requiredScopes"]),
            )
        )

        current_check = "catalog-readme"
        validate_catalog_readme(compatibility_matrix)
        checks.append(check(current_check, path=rel(CATALOG_README_PATH)))

        current_check = "mcp-presets"
        presets_by_id = validate_mcp_presets(
            skills_by_id,
            gateway_tools,
            platform_capabilities,
            args.contracts_mcp_preset_schema,
        )
        context["presets_by_id"] = presets_by_id
        summary["mcpPresets"] = len(presets_by_id)
        checks.append(check(current_check, count=len(presets_by_id)))

        current_check = "agent-profiles"
        profiles_by_id = validate_agent_profiles(
            skills_by_id,
            presets_by_id,
            platform_capabilities,
            args.contracts_agent_profile_schema,
        )
        context["profiles_by_id"] = profiles_by_id
        agent_profile_count = len(profiles_by_id)
        context["agent_profile_count"] = agent_profile_count
        summary["agentProfiles"] = agent_profile_count
        checks.append(check(current_check, count=agent_profile_count))

        current_check = "asset-governance"
        governance_summary = validate_asset_governance(
            skills_by_id,
            presets_by_id,
            profiles_by_id,
            gateway_tools,
            platform_capabilities,
            ai_platform_capabilities,
        )
        summary["assetGovernance"] = governance_summary
        checks.append(check(current_check, path=rel(ASSET_GOVERNANCE_PATH), **governance_summary))

        if args.package_dry_run:
            current_check = "package-dry-run"
            package = package_release_dry_run(args.release_version)
            context["package_summary"] = package
            summary["packageDryRun"] = package
            checks.append(check(current_check, **package))

        if args.package_output_dir is not None:
            current_check = "package-output"
            package = package_release(args.release_version, args.package_output_dir)
            context["package_output_summary"] = package
            summary["packageOutput"] = {
                "artifact": report_path(package["artifactPath"]),
                "manifest": report_path(package["manifestPath"]),
                "checksumFile": report_path(package["checksumPath"]),
                "sha256": package["sha256"],
                "manifestSha256": package["manifestSha256"],
                "checksumFileSha256": package["checksumFileSha256"],
                "files": package["files"],
                "members": package["members"],
            }
            checks.append(
                check(
                    current_check,
                    artifact=report_path(package["artifactPath"]),
                    manifest=report_path(package["manifestPath"]),
                    checksumFile=report_path(package["checksumPath"]),
                    sha256=package["sha256"],
                    manifestSha256=package["manifestSha256"],
                    checksumFileSha256=package["checksumFileSha256"],
                    files=package["files"],
                    members=package["members"],
                )
            )

        if args.verify_package is not None:
            current_check = "package-verify"
            verification = verify_release_package(args.verify_package, args.release_version)
            context["package_verify_summary"] = verification
            summary["packageVerify"] = verification
            checks.append(check(current_check, **verification))
    except ValidationError as exc:
        if current_check and not any(item["name"] == current_check and item["status"] == "failed" for item in checks):
            checks.append(check(current_check, "failed", error=str(exc)))
        report = build_validation_report("failed", args.release_version, checks, summary, str(exc))
        return 1, report, context

    report = build_validation_report("passed", args.release_version, checks, summary)
    return 0, report, context


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate OpenSoha skill assets")
    parser.add_argument("--write-index", action="store_true", help="rewrite skills/index.json from SKILL.md front matter")
    parser.add_argument("--package-dry-run", action="store_true", help="assemble the release tarball in a temporary directory and print its checksum")
    parser.add_argument("--package-output-dir", type=Path, help="write release tarball, manifest, and checksum files to this directory")
    parser.add_argument("--verify-package", type=Path, help="verify an existing release tarball, sibling manifest, and sibling checksum")
    parser.add_argument("--release-version", default="0.1.4", help="release version used for package dry-run metadata")
    parser.add_argument("--report-output", type=Path, help="write a machine-readable validation report JSON artifact")
    parser.add_argument(
        "--contracts-skill-schema",
        type=Path,
        default=default_contracts_skill_schema(),
        help="required same-source path to contracts skills/skill-manifest.schema.json for schema alignment checks",
    )
    parser.add_argument(
        "--contracts-mcp-preset-schema",
        type=Path,
        default=default_contracts_mcp_preset_schema(),
        help="required same-source path to contracts presets/mcp-preset.schema.json for public contract validation",
    )
    parser.add_argument(
        "--contracts-agent-profile-schema",
        type=Path,
        default=default_contracts_agent_profile_schema(),
        help="required same-source path to contracts profiles/agent-profile.schema.json for public contract validation",
    )
    parser.add_argument(
        "--contracts-permission-catalog",
        type=Path,
        default=default_contracts_permission_catalog(),
        help="required same-source path to contracts auth/permission-catalog.json for permission-key conformance",
    )
    parser.add_argument(
        "--gateway-catalog-source",
        type=Path,
        default=DEFAULT_GATEWAY_CATALOG_SOURCE,
        help="required same-source path to Soha Gateway catalog.go for capability snapshot drift checks",
    )
    parser.add_argument(
        "--platform-capability-source",
        type=Path,
        default=DEFAULT_PLATFORM_CAPABILITY_SOURCE,
        help="required same-source path to Soha cluster capabilities.go for platform capability snapshot drift checks",
    )
    args = parser.parse_args()

    exit_code, report, context = run_validation(args)
    write_validation_report(args.report_output, report)
    if exit_code != 0:
        print(f"validation failed: {report['error']}", file=sys.stderr)
        return exit_code

    skills_by_id = context["skills_by_id"]
    presets_by_id = context["presets_by_id"]
    agent_profile_count = context["agent_profile_count"]
    package_summary = context["package_summary"]
    package_output_summary = context["package_output_summary"]
    package_verify_summary = context["package_verify_summary"]

    print(
        "validated "
        f"{len(skills_by_id)} skills, "
        f"{len(presets_by_id)} MCP presets, "
        f"{agent_profile_count} agent profiles"
    )
    if package_summary is not None:
        print(
            f"package dry-run {package_summary['artifact']}: "
            f"sha256={package_summary['sha256']} "
            f"manifestSha256={package_summary['manifestSha256']} "
            f"checksumFileSha256={package_summary['checksumFileSha256']} "
            f"files={package_summary['files']} "
            f"members={package_summary['members']}"
        )
    if package_output_summary is not None:
        print(
            "package wrote "
            f"{package_output_summary['artifactPath']}, "
            f"{package_output_summary['manifestPath']}, "
            f"{package_output_summary['checksumPath']}: "
            f"sha256={package_output_summary['sha256']} "
            f"manifestSha256={package_output_summary['manifestSha256']} "
            f"checksumFileSha256={package_output_summary['checksumFileSha256']} "
            f"files={package_output_summary['files']} "
            f"members={package_output_summary['members']}"
        )
    if package_verify_summary is not None:
        print(
            "package verified "
            f"{package_verify_summary['artifact']}: "
            f"sha256={package_verify_summary['sha256']} "
            f"manifestSha256={package_verify_summary['manifestSha256']} "
            f"checksumFileSha256={package_verify_summary['checksumFileSha256']} "
            f"files={package_verify_summary['files']} "
            f"members={package_verify_summary['members']}"
        )
    return 0


if __name__ == "__main__":
    guard = shutil.which("opensoha-verify")
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            and not os.environ.get("OPENSOHA_VERIFY_ACTIVE")
            and (guard or os.environ.get("OPENSOHA_LOCAL_VERIFY_REQUIRED"))):
        os.execvp(guard or "opensoha-verify", [guard or "opensoha-verify", "run", "--", sys.executable, __file__, *sys.argv[1:]])
    raise SystemExit(main())
