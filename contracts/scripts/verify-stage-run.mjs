import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";

export function validateStageRun(run, workflow, { repository, tag, sha, runId }) {
  assert.equal(repository, "opensoha/soha");
  assert.match(runId ?? "", /^[1-9][0-9]*$/);
  assert.equal(String(run.id), runId);
  assert.equal(run.repository?.full_name, repository);
  assert.equal(run.head_repository?.full_name, repository);
  assert.equal(workflow.path, ".github/workflows/contracts-release.yml");
  assert.equal(run.workflow_id, workflow.id);
  assert.equal(run.path, workflow.path);
  assert.equal(run.head_sha, sha);
  assert.equal(run.head_branch, tag);
  assert(["push", "workflow_dispatch"].includes(run.event));
  assert.equal(run.status, "completed");
  assert.equal(run.conclusion, "success");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const identity = { repository: process.env.GITHUB_REPOSITORY, tag: process.env.RELEASE_TAG, sha: process.env.RELEASE_SHA, runId: process.env.STAGE_RUN_ID };
  assert.match(identity.runId ?? "", /^[1-9][0-9]*$/);
  const api = `https://api.github.com/repos/${identity.repository}/actions`;
  const get = async (path) => {
    const response = await fetch(`${api}/${path}`, { headers: { Authorization: `Bearer ${process.env.GH_TOKEN}`, Accept: "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28" }, signal: AbortSignal.timeout(60_000) });
    if (!response.ok) throw new Error(`Cannot verify original stage run: HTTP ${response.status}`);
    return response.json();
  };
  const [run, workflow] = await Promise.all([get(`runs/${identity.runId}`), get("workflows/contracts-release.yml")]);
  validateStageRun(run, workflow, identity);
  console.log(`Verified original stage run ${identity.runId} at ${identity.tag} / ${identity.sha}`);
}
