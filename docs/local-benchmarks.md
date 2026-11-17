# Local evaluations

Disabled by default. Admin → Server enables the feature. Each user must then
consent in Settings → Account to collecting **and replaying sanitized tasks to
additional configured free providers**, and opt in individual API keys. Shared
spaces cannot change another user's consent. Pool/peer requests and models,
internal gateway turns, failed answers and incomplete streams are excluded.

## Data boundary

The gateway hooks completed canonical answers before restoration of privacy
placeholders. The existing privacy pipeline runs again for capture, with every
category enabled and no allow-list bypass, even if normal request processing is
off. If the optional local privacy model is enabled, all message roles and tool
context are scanned, without using the shared entity cache; failures reject the
sample. Attachments, reasoning traces and high-sensitivity material are rejected.
A semantic suitability review must also approve the task before candidate replay.
Sanitization reduces risk; it is not a guarantee that arbitrary prose contains no
private meaning. Collection and additional-provider replay require explicit consent.

Staging payloads and checkpoints are AES-GCM encrypted with a user-derived key
and authenticated owner identity. Queries are owner-scoped, and another owner's
key cannot decrypt a record. Prompt hashes use per-user HMAC domains, not public
cross-user hashes. No raw samples are exposed through ordinary admin or results
APIs. There is no central dataset, synchronization or upload service. Provider
inference calls can leave the machine, only through that user's configured
providers and key model restrictions.

Private samples expire after 1, 7 or 30 days (default 7). Completed and rejected
jobs discard their content immediately. Disabling consent cancels active work
and deletes affected staged records; generation tickets also invalidate already
queued capture callbacks. Users can delete their own samples and numeric
contributions, recalculating the affected global aggregates. Account deletion
also removes them. Numeric evidence expires after 90 days; each model/task group
keeps at most 512 recent contributions. Database backups follow the operator's
own retention policy; row deletion does not promise forensic erasure of backups.

## Execution

A separately scheduled worker advances one durable step at a time. It rechecks
all permissions, model restrictions and sample expiry before and after calls.
Expired job leases recover after restart; encrypted checkpoints preserve completed
steps. A crash during an uncheckpointed inference may repeat that inference.
Quota reservations are flushed before dispatch to keep retries accounted for.

Evaluation uses existing provider adapters, free classification, routing
eligibility, account pools, health, quotas, priority and demand/reset pacing.
Manual free overrides, paid models, pool models and every prepaid/subscription
account are excluded. Unknown pricing is not promoted to free. A reported charge
invalidates the free classification and rejects the job. No benchmark-specific
quota percentage, reserve or production throttle is exposed.

Foreground work takes priority immediately: new production work aborts an active
benchmark call. Benchmark leases participate in actual concurrency but do not
train slot utilization. Real quota usage and provider reset headers are accounted
for; uncertain interrupted calls retain conservative token reservations. The
benchmark path bypasses the gateway executor, production request analytics,
retry feedback, speed/reliability statistics and task experience learning.

## Evaluation and routing

At least four distinct eligible free models are needed: two candidates and two
independent judges. Candidate selection does not use internal benchmark results.
Judge selection uses external intelligence only. The original production answer
is captured for suitability review, not treated as ground truth or replay grading.

Deterministic output constraints reject malformed JSON/tool calls and incomplete
answers. Each judge must pass generated known-answer and uncertainty calibration.
A versioned, strict rubric penalizes unsupported claims, hallucinations,
instruction failures and unjustified confidence, without rewarding verbosity.
Judges see anonymous answers in both orders; all four high-confidence verdicts
must agree. Ties are allowed; abstention, conflicting verdicts and uncertain or
malformed output produce no global evidence. No arbitrary user code is executed.

Only after eight distinct validated tasks per model/category are aggregates
published. Global rows contain model/task identifiers, preference rate, sample
count, confidence, evaluator version, judge identifiers and recency; no user IDs,
prompts, answers, sample hashes or per-user results. Pairwise preference is not
absolute accuracy. Confidence- and freshness-weighted evidence supplements the
existing task-specific intelligence score, with a bounded share and a strong
sample-size prior. Missing evidence leaves routing unchanged. It never replaces
external benchmarks, online quality learning, reliability, latency, cost or quotas.
