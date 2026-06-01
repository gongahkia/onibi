# DFIR-Metric Practical Benchmark

KelpClaw's DFIR-Metric adapter targets Module III, the NIST Forensic String Search practical set published by the DFIR-Metric project.

## Dataset Source

- Paper: `arxiv:2505.19973v1`, "DFIR-Metric: A Benchmark Dataset for Evaluating Large Language Models in Digital Forensics and Incident Response"
- Official repository: <https://github.com/DFIR-Metric/DFIR-Metric>
- Practical dataset file: `DFIR-Metric-NSS.json`
- Pinned raw URL: <https://raw.githubusercontent.com/DFIR-Metric/DFIR-Metric/main/DFIR-Metric-NSS.json>
- Pinned SHA-256: `c180284ffd249d16813050690f1da5328f41b742372905205d03851e45e5dc7f`
- License: no repository license is declared upstream. The GitHub API reports `license: null`, and `LICENSE` returns 404 as of 2026-05-31.

## Acquisition

Run:

```sh
kelp-claw findevil benchmark --dataset dfir-metric --subset-size 10 --out .kelpclaw/findevil/benchmark/dfir-metric/
```

The adapter downloads `DFIR-Metric-NSS.json` into `.kelpclaw/datasets/dfir-metric/`, verifies it against the pinned SHA-256 manifest embedded in `packages/findevil/src/benchmark/dfir-metric.ts`, and reuses the cached copy when the hash matches.

## Outputs

The benchmark command writes one case directory per selected DFIR-Metric case. The default mode is `blind-trace-no-answer-evidence`: expected answer values stay in the scorer only and are not written into evidence files or trace claims. Cases with non-empty expected answers contain the generated case manifest, evidence scaffold, sentinel artifacts, `benchmark-report.json`, and `accuracy-report.md`; empty-answer cases are scored directly without invoking the sentinel.

The aggregate files are:

- `.kelpclaw/findevil/benchmark/dfir-metric/aggregate-report.json`
- `.kelpclaw/findevil/benchmark/dfir-metric/aggregate-accuracy-report.md`

The aggregate report includes total precision, recall, and F1, plus per-category precision, recall, and F1.

## Citation

```bibtex
@inproceedings{cherif2025dfir,
  title={Dfir-metric: A benchmark dataset for evaluating large language models in digital forensics and incident response},
  author={Cherif, Bilel and Bisztray, Tamas and Dubniczky, Richard A and Aldahmani, Aaesha and Alshehhi, Saeed and Tihanyi, Norbert},
  booktitle={International Conference on Neural Information Processing},
  pages={17--31},
  year={2025},
  organization={Springer}
}
```

## Cost Notes

The default adapter path does not download NIST disk images or call a model API. It runs the Kelp sentinel pipeline in trace mode against deterministic per-case scaffolds so CI and local smoke runs stay cheap and reproducible.

Full practical reproduction against the original NIST images is a heavier workflow: disk image acquisition, forensic tooling, and live agent/model calls may add storage, network, and model-token costs.
