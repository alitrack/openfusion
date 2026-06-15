# Spec: Judge — Multi-Model Analysis & Synthesis

## Structured Analysis

### Scenario: All models agree

Given panel responses contain no factual contradictions
When Judge analyzes them
Then consensus list contains the shared conclusions
And contradictions list is empty
And unique_insights may contain additional depth from singular models

### Scenario: Models disagree

Given model A says "temperature rise is 1.5°C" and model B says "temperature rise is 1.2°C"
When Judge analyzes them
Then contradictions list contains {"issue": "temperature rise value", "A": "1.5°C", "B": "1.2°C"}
And final answer acknowledges the discrepancy

### Scenario: Partial coverage

Given only model C covers topic X
When Judge analyzes
Then partial_coverage includes "topic X covered only by model C"
And final answer includes topic X

### Scenario: All models miss something obvious

Given none of the panel models mention topic Y
When Judge analyzes
Then blind_spots includes "all models did not address topic Y"
And Judge's final answer may still not cover Y (Judge doesn't hallucinate)

## Final Synthesis

### Scenario: Judge writes synthesized answer

Given Judge has completed structured analysis
When Judge produces final answer
Then the answer is a coherent narrative (not bullet-point comparison)
And it references consensus findings
And it notes contradictions where they exist
And it acknowledges blind spots

### Scenario: Judge model timeout

Given the Judge model takes > Judge_timeout to respond
When the orchestrator detects the timeout
Then it returns an error "judge timeout" with HTTP 503
And the partial panel responses are not returned
