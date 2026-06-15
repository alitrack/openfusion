# Spec: Panel — Parallel Model Dispatch

## Parallel Dispatch

### Scenario: 3-model panel, all succeed

Given a panel configured with 3 models (modelA, modelB, modelC)
When the Fusion engine dispatches a prompt
Then all 3 models are called concurrently
And each receives the full original messages
And each returns its own response
And no model's response is visible to other panel members
And total wall time ≈ max(latency_A, latency_B, latency_C) + Judge time

### Scenario: One model times out

Given a panel with 3 models and `panel_timeout: 5s`
When model C takes >5s to respond
Then model C is marked as "timed out"
And panel dispatch completes with responses from model A and B only
And Judge is invoked with the partial response set

### Scenario: One model returns error

Given a panel with 3 models
When model B returns HTTP 500
Then model B is marked as "failed"
And panel dispatch continues with model A and C
And Judge is invoked noting "model B failed"

## Context Isolation

### Scenario: No cross-contamination

Given models A and B are called in parallel
When model A receives the prompt
Then model A cannot see model B's response
And model B cannot see model A's response
