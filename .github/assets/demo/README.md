# Engineering Intelligence overview

This illustrated overview presents actual outputs from independent runnable examples. It is
not a shipped dashboard, one connected dataset or an automatic pipeline. Inputs are synthetic
fixtures; scores, confidence and estimated ROI are example results, not customer measurements.

| Scene | Examples and retained output |
|---|---|
| Overview | Ten packages, grouped into five questions; see the root README's package table |
| Contribution | [attribute-across-promotions](../../../examples/attribute-across-promotions/) → `promotions.txt`; [attribute-issues-to-ai](../../../examples/attribute-issues-to-ai/) → `contribution.txt` |
| Work | [score-a-diff](../../../examples/score-a-diff/) → `work.txt` |
| Quality | [trace-one-bug-fix](../../../examples/trace-one-bug-fix/) → `quality.txt` |
| Value | [roi-demo](../../../examples/roi-demo/) → `value.txt` |
| Maturity | [fold-team-facts](../../../examples/fold-team-facts/) → `maturity.txt` |
| Insufficient evidence | [roi-on-a-thin-window](../../../examples/roi-on-a-thin-window/) → `insufficient.txt` |

The contribution scene combines two explicitly separate examples. AI signatures mark PR authorship
in the issue example; the promotion example demonstrates session attribution and branch lineage.
One of two linked issues is AI-assisted, but only two of four issues have usable links. That
coverage matters as much as the share. `session.txt` retains the separate minimal
[match-one-session](../../../examples/match-one-session/) result as additional evidence.

The work scene demonstrates effective diff size and relative complexity. Type/domain labeling
is another capability, but it needs the companion `model-service`; no classification call is
shown. Complexity does not measure developer skill.

The quality example skips a formatting commit and ranks an earlier introducing candidate with
confidence 0.531. Its comparator uses file-level context, so the result is explicitly degraded.
This is evidence for a candidate, not proof of causation.

The ROI example uses synthetic feature and bugfix cohorts. Its 203 recovered hours over 30 days
and $185,238 annual value are model estimates under a $75/hour calibration. The thin-window
example shows why consumers should read the basis first and withhold dollars for insufficient
data. Differences between cohorts do not establish a causal AI effect.

The maturity example reports different frontiers across lifecycle stages and a planning
bottleneck. Unmeasured stages are not rendered as zero maturity.

## Refresh

From the repo root, capture each example's stdout, for example:

```sh
go run ./examples/attribute-across-promotions > .github/assets/demo/promotions.txt
go run ./examples/attribute-issues-to-ai > .github/assets/demo/contribution.txt
go run ./examples/score-a-diff > .github/assets/demo/work.txt
go run ./examples/trace-one-bug-fix > .github/assets/demo/quality.txt
go run ./examples/roi-demo > .github/assets/demo/value.txt
go run ./examples/fold-team-facts > .github/assets/demo/maturity.txt
go run ./examples/roi-on-a-thin-window > .github/assets/demo/insufficient.txt
go run ./examples/match-one-session > .github/assets/demo/session.txt
scripts/run-examples.sh
python3 scripts/render-readme-demo.py --preview-dir /tmp/engineering-demo
```

The examples need Go and no account, key or model. Rendering needs Pillow plus Arial/Menlo on
macOS or DejaVu on Linux. `scenes.json` holds the narration and exact evidence excerpts; the
renderer rejects missing excerpts and text that exceeds its layout. If example results change,
update the narration and evidence together, then visually inspect every exported scene.
