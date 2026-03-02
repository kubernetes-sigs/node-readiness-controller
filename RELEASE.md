# Release Process

This document outlines the process for creating a new official release for the Node Readiness Controller. The repository uses an automated release pipeline to handle branching and tagging.

## 1. Propose the Release

- Create a new GitHub Issue titled "Release vX.Y.Z" to track the release process.
- In the issue, gather a preliminary changelog by reviewing commits since the last release. A good starting point is `git log --oneline <last-tag>..HEAD`.

## 2. Trigger the Release Automation

Instead of manually creating branches and tags, the release is triggered by a pull request:

- The Release Shepherd creates a new PR targeting the `main` branch (for minor/major releases) or a `release-*` branch (for patch releases).
- In this PR:
  - Update the `VERSION` file at the repository root to the new semantic version (e.g., `v0.2.0`).
  - Update any documentation (like `docs/book/src/releases.md`), examples, or manifests as needed for the release.
- Ensure all tests are passing.
- Once the PR is merged, the Release Automation GitHub Action will trigger.

The automation will:
1. Validate the semantic version format.
2. Create and push a git tag `vX.Y.Z` at the merged commit.
3. If it is a minor or major release (e.g. `v0.2.0`), it will automatically branch off `release-vX.Y.Z` and push it to the repository.

## 3. Create the GitHub Release

- Pushing the tag will trigger the `cloudbuild.yaml` CI to build and publish the container image for the release (e.g., `us-central1-docker.pkg.dev/.../node-readiness-controller:vX.Y.Z`).
- Go to the [Releases page](https://github.com/kubernetes-sigs/node-readiness-controller/releases) on GitHub.
- Find the new tag and click "Edit tag" (or "Draft a new release" and select the tag).
- Paste the final changelog into the release description.
- Generate the release manifests locally for this version:

  ```sh
  make build-installer IMG_PREFIX=registry.k8s.io/node-readiness-controller/node-readiness-controller IMG_TAG=vX.Y.Z
  ```
- Upload the generated `dist/crds.yaml`, `dist/install.yaml`, and `dist/install-full.yaml` files as release artifacts.
- Publish the release.

## 4. Post-Release Tasks

- Close the release tracking issue.
- Announce the release on the `sig-node` mailing list. The subject should be: `[ANNOUNCE] Node Readiness Controller vX.Y.Z is released`.

