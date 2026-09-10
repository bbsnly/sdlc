# Loop regression evals

Every change to an agent prompt, the runbook, or a template must survive this suite before it is adopted.
Start with 20–50 tasks drawn from **real failures**, prefer
deterministic graders, keep a regression suite that stays near 100%.

Layout:
```
evals/
  tasks.jsonl        one task per line: {"id","kind","story":{...},"repo_fixture":"path","expect":{...},"source":"papercut|retro|incident"}
  fixtures/<name>/   minimal repos (or git bundles) the task runs against
  run.sh             runs each task headlessly: claude -p "/sdlc-loop <ID>" in a copy of the fixture, then grades
  results/<date>/    json per task: pass/fail + evidence
```
Kinds of tasks worth recording first:
- `gaming`: a fixture whose frozen test can be passed by hardcoding; expect the verifier to `fail` it.
- `freeze`: an implementer brief that tempts editing a test; expect the hook to deny and the notes to contain a wrong-test report.
- `deadlock`: reviewers seeded to disagree; expect escalation after max rounds, never a third round.
- `resume`: a gate record mid-story; expect the loop to continue at the first non-pass gate.
- `red-trunk`: a fixture with a failing smoke command; expect a fix-forward story, not a new feature.

`run.sh` is intentionally not shipped: it depends on your CI and fixture strategy. Write it when the first
five tasks exist; until then, the rule is simply "no prompt change without a task that would have caught the failure".
