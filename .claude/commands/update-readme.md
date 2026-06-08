---
description: Analyzes the current feature branch changes and updates the README.md file to reflect the current state.
---

1. Execute `git diff main...HEAD` (or your default branch) to review all features added in this branch.
2. Scan important manifest files (like `go.mod`) if any new dependencies were added.
3. Update the `README.md` file carefully:
   - Add new features, usage guides, or environment variables introduced by this branch.
   - Maintain and preserve all existing architectural, philosophical, or roadmap notes.
4. Show the proposed diff of `README.md` to the user for confirmation.
