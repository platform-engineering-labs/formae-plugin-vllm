# Changelog

All notable changes to the formae vLLM plugin are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Install with `sudo formae plugin install vllm` on the host that runs the
formae agent.

## [0.1.1]

### Added

- Initial release of the vLLM plugin as a standalone package built on the formae
  Plugin SDK. Declaratively manage LoRA adapters on a running vLLM server and
  discover the base models it serves, with offline-aware reconciliation for edge
  and air-gapped inference fleets.
