![CODED BY](https://img.shields.io/badge/CODED%20BY-LLM-d29922?style=for-the-badge)

# fibre

# Installation

```sh
curl -fsSL https://raw.githubusercontent.com/wetsocksnsleeves/fibre/main/install.sh | sh
```

The script downloads the binary for your OS (macOS or Linux, amd64 or arm64)
from the latest GitHub release and installs it to `~/.local/bin/fibre`. To
pick a different release or directory:

```sh
curl -fsSL https://raw.githubusercontent.com/wetsocksnsleeves/fibre/main/install.sh \
  | FIBRE_VERSION=v0.1.0 FIBRE_INSTALL_DIR=/usr/local/bin sh
```

To publish a release that the script can install:

```sh
git tag v0.1.0 && git push origin v0.1.0
make dist VERSION=v0.1.0
gh release create v0.1.0 dist/*
```

# Usage
