## Documentation (mdBook)

This repository’s documentation is built using [**mdBook**](https://github.com/rust-lang-nursery/mdBook).

Local builds and hosted builds use the same build script [1] to ensure consistent behavior.

## Local documentation workflows

All documentation targets **must be run from the repository root**.

### Available Makefile targets

```bash
make docs        # Build the mdBook locally using the shared build script
make docs-serve  # Serve the mdBook locally
```

## Build script contract

The documentation build is driven by:

```
docs/book/install-and-build-mdbook.sh
```

This script defines how mdBook is installed and executed.

### Environment variables

The build relies on the following environment variables:

| Variable         | Description                      |
| ---------------- | -------------------------------- |
| `GO_VERSION`     | Go version used during the build |
| `MDBOOK_VERSION` | mdBook version to install/use    |


## Version synchronization requirements

The environment variables used for documentation builds must be kept **consistent across the repository**.

When either `GO_VERSION` or `MDBOOK_VERSION` is changed, the update **must be applied in all of the following locations**:

1. **Makefile**
   Used for local documentation builds and serving.

2. **`netlify.toml`**
   Used for hosted and preview builds.

3. **`docs/book/install-and-build-mdbook.sh`**
   Used as the canonical build script.

## Editing documentation

1. Make changes under:

   ```
   docs/book/src/
   ```
2. Preview changes locally:

   ```bash
   make docs-serve
   ```
3. Commit only source files; generated output is produced during the build.

---

[1] [`docs/book/install-and-build-mdbook.sh`](book/install-and-build-mdbook.sh)

