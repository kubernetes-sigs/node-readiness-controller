# Release Process

This document outlines the process for creating a new official release for the Node Readiness Controller.

## 1. Propose the Release

- Create a new GitHub Issue titled "Release vX.Y.Z" to track the release process.
- In the issue, gather a preliminary changelog by reviewing commits since the last release. A good starting point is `git log --oneline <last-tag>..HEAD`.

## 2. Prepare the Release Branch

- Create a new branch from `main` named `release-vX.Y.Z`.
- In this branch, update any documentation, examples, or manifests as needed for the release.
- Ensure all tests are passing.

## 3. Tag the Release

Once the release branch is ready and has been approved by maintainers:

- Make sure your local `main` branch is up to date and you have the release branch checked out.
- Create an annotated, signed Git tag for the version. The tag message should contain the final changelog.

  ```sh
  # Example:
  VERSION=v0.1.0
  git tag -s "${VERSION}" -m "Release ${VERSION}

  ### Release Notes
  - Initial alpha release of the controller.
  - Support for NodeReadinessRule API.
  - Manages node taints based on custom readiness conditions."
  ```

- Push the tag to the main repository:

  ```sh
  git push origin "${VERSION}"
  ```

## 4. Create the GitHub Release

- Pushing the tag will trigger the `cloudbuild.yaml` CI to build and publish the container image for the release (e.g., `us-central1-docker.pkg.dev/.../node-readiness-controller:vX.Y.Z`).
- Go to the [Releases page](https://github.com/kubernetes-sigs/node-readiness-controller/releases) on GitHub.
- Find the new tag and click "Edit tag".
- Paste the same changelog into the release description.
- Generate the release manifest locally for this version:

  ```sh
  make build-installer IMG_PREFIX=registry.k8s.io/node-readiness-controller/node-readiness-controller IMG_TAG=vX.Y.Z
  ```
- Upload the generated `dist/install.yaml` file as a release artifact.
- Publish the release.

## 5. Post-Release Tasks

- Close the release tracking issue.
- Announce the release on the `kubernetes-dev` and `sig-node` mailing lists. The subject should be: `[ANNOUNCE] Node Readiness Controller vX.Y.Z is released`.

