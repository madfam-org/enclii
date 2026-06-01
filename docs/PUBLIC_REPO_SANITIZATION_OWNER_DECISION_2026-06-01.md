# Enclii Public Repo Sanitization Owner Decision

Date: 2026-06-01
Current status: blocked, not sanitized

## Evidence summary

- Current-tree exact credential-signature paths: 0
- Git-history matched paths: 6
- GitHub Actions artifacts reported: 2740
- Releases page count: 1

## Required owner decisions

- Choose `history_rewrite` or `risk_acceptance_plus_revocation` for history matches.
- Choose `artifact_body_review`, `artifact_retention_cleanup`, or `artifact_risk_acceptance` for public artifacts.
- Confirm no kubeconfig, cloud credential, reusable secret, tenant identifier, exploit-ready topology, or privileged operational procedure exists in public source/history/artifacts.
- Approve or reject whether Enclii can produce `PUBLIC_GITHUB_REPO_SANITIZED` Tulana evidence.

## Recommended decision

Keep status blocked until archived audits, infra artifacts, and history matches receive owner disposition.

## Artifact retention evidence update

Current-tree workflow audit found zero checked workflows using `actions/upload-artifact`, so no current workflow retention edit was applied in this pass. Existing GitHub artifact volume remains launch-blocking.

Owner still needs to choose artifact body review, artifact retention cleanup, or explicit time-bounded artifact risk acceptance.

## Full artifact metadata update

- Total artifacts: 2,740
- Active artifacts: 577
- Expired artifacts: 2,163
- Total artifact bytes: 136,410,149,330
- Risk-name artifacts: 368
- Active risk-name artifacts: 0
- Risk-name artifact bytes: 616,809,670

Owner review should start with the largest active artifacts and then archived audit/infrastructure risk-name artifacts. Enclii is the highest-risk repo in this pass by total artifact bytes.
