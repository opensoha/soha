#!/usr/bin/env bash
set -euo pipefail

if [[ "${GITHUB_ACTIONS:-}" != true && -z "${OPENSOHA_VERIFY_ACTIVE:-}" ]] &&
  { command -v opensoha-verify >/dev/null 2>&1 || [[ -n "${OPENSOHA_LOCAL_VERIFY_REQUIRED:-}" ]]; }; then
  exec opensoha-verify run -- bash "$0" "$@"
fi

archive="${1:-}"
if [[ -z "${archive}" ]]; then
  echo "usage: $0 <release-archive.tar.gz|release-archive.zip>" >&2
  exit 2
fi
if [[ ! -f "${archive}" ]]; then
  echo "release archive not found: ${archive}" >&2
  exit 1
fi

workdir="${SOHA_RELEASE_SMOKE_WORKDIR:-$(mktemp -d)}"
cleanup() {
  if [[ -z "${SOHA_RELEASE_SMOKE_WORKDIR:-}" ]]; then
    rm -rf "${workdir}"
  fi
}
trap cleanup EXIT

extract_dir="${workdir}/extract"
mkdir -p "${extract_dir}"
case "${archive}" in
  *.tar.gz)
    tar -xzf "${archive}" -C "${extract_dir}"
    ;;
  *.zip)
    unzip -q "${archive}" -d "${extract_dir}"
    ;;
  *)
    echo "unsupported release archive format: ${archive}" >&2
    exit 1
    ;;
esac

soha_bin="$(find "${extract_dir}" -type f \( -name soha -o -name soha.exe \) -print -quit)"
if [[ -z "${soha_bin}" ]]; then
  echo "release archive does not contain a soha binary" >&2
  exit 1
fi
chmod +x "${soha_bin}"

version_json="${workdir}/version.json"
"${soha_bin}" version --json > "${version_json}"
grep -q '"version"' "${version_json}"
grep -q '"commit"' "${version_json}"

mcp_json="${workdir}/mcp.json"
"${soha_bin}" mcp install \
  --profile release-smoke \
  --command "${soha_bin}" \
  --ai-client-id release-smoke \
  --ai-client "Release Smoke" \
  --skill-id k8s-sre > "${mcp_json}"
grep -q "\"command\": \"${soha_bin}\"" "${mcp_json}"
grep -q '"release-smoke"' "${mcp_json}"
grep -q '"k8s-sre"' "${mcp_json}"
if grep -q '"start"' "${mcp_json}"; then
  echo "mcp install emitted the legacy start subcommand" >&2
  exit 1
fi

home="${workdir}/home"
runtime_skills="${workdir}/runtime-skills"
mkdir -p "${home}" "${runtime_skills}"

skills_source="${workdir}/skills-source"
skills_arg="k8s-sre"
if [[ -n "${SOHA_SKILLS_ARTIFACT:-}" ]]; then
  if [[ ! -f "${SOHA_SKILLS_ARTIFACT}" ]]; then
    echo "skills artifact not found: ${SOHA_SKILLS_ARTIFACT}" >&2
    exit 1
  fi
  artifact_dir="$(cd "$(dirname "${SOHA_SKILLS_ARTIFACT}")" && pwd)"
  artifact_name="$(basename "${SOHA_SKILLS_ARTIFACT}")"
  artifact_path="${artifact_dir}/${artifact_name}"
  skills_source="${artifact_path}"
  skills_arg="all"
else
  mkdir -p "${skills_source}/k8s-sre" "${skills_source}/agent-skills/soha/agents"
  cat > "${skills_source}/k8s-sre/SKILL.md" <<'EOF'
---
name: k8s-sre
description: Release smoke fixture.
---

# K8s SRE
EOF
  cat > "${skills_source}/agent-skills/soha/SKILL.md" <<'EOF'
---
name: soha
description: Use Soha MCP and governed delivery workflows.
---

# Soha
EOF
  cat > "${skills_source}/agent-skills/soha/agents/openai.yaml" <<'EOF'
interface:
  display_name: "Soha"
  short_description: "Soha workflows"
  default_prompt: "Use $soha for delivery work."
EOF
fi

add_output="${workdir}/add-all.txt"
HOME="${home}" \
SOHA_CONFIG="${workdir}/soha-config.json" \
SOHA_SKILLS_DIR="${runtime_skills}" \
SOHA_SKILLS_CACHE="${workdir}/skills-cache" \
"${soha_bin}" setup all \
  --dry-run \
  --profile release-smoke \
  --server https://mcp.opensoha.com \
  --source "${skills_source}" \
  --command "${soha_bin}" \
  --skills "${skills_arg}" \
  --runtime-skill-dest "${runtime_skills}" > "${add_output}"

for target in Codex Claude Cursor Kiro Gemini Antigravity Trae; do
  grep -q "Would update ${target} MCP config" "${add_output}"
  grep -q "Would install ${target} Soha skill package" "${add_output}"
done
grep -q "Would install Soha runtime skills" "${add_output}"

HOME="${home}" \
SOHA_SKILLS_CACHE="${workdir}/skills-cache" \
"${soha_bin}" skill install \
  --source "${skills_source}" \
  --dest "${runtime_skills}" \
  --all > "${workdir}/skill-install.txt"
HOME="${home}" \
"${soha_bin}" skill status --dest "${runtime_skills}" --json > "${workdir}/skill-status.json"
grep -q '"managed": true' "${workdir}/skill-status.json"
HOME="${home}" "${soha_bin}" skill remove --dest "${runtime_skills}" k8s-sre > "${workdir}/skill-remove.txt"
test ! -e "${runtime_skills}/k8s-sre/SKILL.md"
HOME="${home}" "${soha_bin}" skill rollback --dest "${runtime_skills}" > "${workdir}/skill-rollback.txt"
test -f "${runtime_skills}/k8s-sre/SKILL.md"

agent_skills="${workdir}/agent-skills"
HOME="${home}" \
SOHA_CONFIG="${workdir}/soha-config.json" \
SOHA_SKILLS_CACHE="${workdir}/skills-cache" \
"${soha_bin}" setup --client codex \
  --mode skill \
  --profile release-smoke \
  --source "${skills_source}" \
  --skills "${skills_arg}" \
  --dest "${agent_skills}" \
  --no-runtime-skills \
  --command "${soha_bin}" > "${workdir}/setup-skill.txt"
test -f "${agent_skills}/soha/SKILL.md"
test -f "${agent_skills}/soha/agents/openai.yaml"
grep -q 'name: soha' "${agent_skills}/soha/SKILL.md"

echo "release smoke passed for ${archive}"
