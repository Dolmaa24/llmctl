# 🟡 Phase 2: Sanket's Laptop Tuning

This folder is dedicated to scripts, benchmarks, and configurations required to tune the `llmctl` extraction engine for Sanket's specific Windows/Ollama laptop hardware.

By isolating these files here, we keep Phase 1 (Core Engine) and Phase 2 (Local Laptop Tuning) completely separate and easy to manage!

## Tasks to Complete Here:

1. **Model Benchmarking:** Scripts to test 7B-14B local models.
2. **Dedicated Extraction Target:** Setting the exact local model `llmctl` will use for taking notes in the background.
3. **Scheduling UX:** Scripts to test `Continuous` vs `AtSwitch` performance on the laptop's GPU.
4. **Context Window Tuning:** Tuning the `DefaultRecentTurns` variable.
5. **Final Demo Metrics:** Storing the final speed and token savings here for the Capstone report.
