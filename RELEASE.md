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
  - Update `docs/book/src/releases.md` with the release notes.
  - Update any other documentation, examples, or manifests as needed for the release.
- Ensure all tests are passing.
- Once the PR is merged, the Release Automation GitHub Action will trigger.

The automation will:
1. Validate the semantic version format.
2. Create and push a git tag `vX.Y.Z` at the merged commit.
3. If it is a minor or major release (e.g. `v0.2.0`), it will automatically branch off `release-vX.Y.Z` and push it to the repository.

## 3. Promote the Images and Helm Chart

After the release tag is pushed, the images and Helm chart must be built, pushed to staging, and then promoted to the official registry.

### A. Verify Staging Images and Chart
- Monitor the push job status on [Testgrid (sig-node-image-pushes)](https://testgrid.k8s.io/sig-node-image-pushes#post-node-readiness-controller-push-images).
- The Prow job configuration is in [test-infra](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-node-readiness-controller.yaml) if troubleshooting is needed.
- Verify the images exist in the staging repository:
  ```sh
  skopeo list-tags docker://us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/node-readiness-controller | grep vX.Y.Z
  ```
- Verify the chart exists in the staging repository (replace `<chart-version>` with the version in `charts/node-readiness-controller/Chart.yaml` at the tagged commit):
  ```sh
  helm pull oci://us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/charts/node-readiness-controller --version <chart-version>
  ```

### B. Create PR for Promotion
- Identify the image digests:
  ```sh
  skopeo inspect docker://us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/node-readiness-controller:vX.Y.Z --format '{{.Digest}}'
  skopeo inspect docker://us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/node-readiness-reporter:vX.Y.Z --format '{{.Digest}}'
  ```
- Identify the chart digest:
  ```sh
  skopeo inspect --raw docker://us-central1-docker.pkg.dev/k8s-staging-images/node-readiness-controller/charts/node-readiness-controller:<chart-version> | sha256sum
  ```
  Alternatively, note the digest printed by `helm push`/`helm pull` when the chart was published/verified above.
- Fork [kubernetes/k8s.io](https://github.com/kubernetes/k8s.io).
- Update
  `registry.k8s.io/images/k8s-staging-node-readiness-controller/images.yaml`
  with the new digests and tags for the images and the chart ([sample PR](https://github.com/kubernetes/k8s.io/pull/9176)).
- Submit the PR to `kubernetes/k8s.io`. Once it is approved and merged
  automation will schedule the promotion.

## 4. Final Verification and GitHub Release

Before publishing the release, verify the images and chart are available at k8s-registry:

### A. Verify Official Registry
- Ensure the images are available at `registry.k8s.io`:
  ```sh
  skopeo list-tags docker://registry.k8s.io/node-readiness-controller/node-readiness-controller | grep vX.Y.Z
  ```
- Ensure the chart is available at `registry.k8s.io`:
  ```sh
  helm pull oci://registry.k8s.io/node-readiness-controller/charts/node-readiness-controller --version <chart-version>
  ```

### B. Test Release Artifacts
- Checkout the release tag locally
  ```sh
  git show vX.Y.Z -q  # to verify the right tag and commit
  git checkout vX.Y.Z
  ```
- Generate the release manifests:
  ```sh
  rm -rf ./dist
  make build-installer IMG_PREFIX=registry.k8s.io/node-readiness-controller/node-readiness-controller IMG_TAG=vX.Y.Z
  ls ./dist   # to confirm the generated artifacts
  ```
- Run a local E2E test in Kind (see `docs/TEST_README.md`) using the generated manifests from the `dist/` directory to ensure they pull the correct images and function as expected.

### C. Create the GitHub Release
- Go to the [Releases page](https://github.com/kubernetes-sigs/node-readiness-controller/releases) on GitHub.
- Find the new tag and click "Edit tag" (or "Draft a new release" and select the tag).
- Paste the final changelog into the release description.
- Upload the generated  manifests (`dist/crds.yaml`, `dist/install.yaml`, and `dist/install-full.yaml`) as release artifacts.
- Publish the release.

## 5. Post-Release Tasks

- Close the release tracking issue.
- Announce the release on the `sig-node` mailing list. The subject should be: `[ANNOUNCE] Node Readiness Controller vX.Y.Z is released`.

