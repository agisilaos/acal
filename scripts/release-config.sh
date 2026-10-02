#!/usr/bin/env bash
# Repository-owned settings; shared helpers are pinned to this reviewed bundle.
CLI_NAME="acal"
FORMULA_NAME="acal"
ARTIFACT_NAME="acal"
DEFAULT_BRANCH="main"
DEFAULT_HOMEBREW_DESC="Apple Calendar CLI for terminal workflows and agents"
DEFAULT_HOMEBREW_LICENSE="MIT"
DEFAULT_HOMEBREW_TEST_ARG="version"
DEFAULT_FORMULA_PATH="Formula/acal.rb"
DEFAULT_BUILD_PKG="./cmd/acal"
RELEASE_LDFLAGS_TEMPLATE='-s -w -X main.version={{VERSION}} -X main.commit={{COMMIT}} -X main.date={{DATE}}'
RELEASE_VERSION_TEMPLATE='acal {{VERSION}} ({{COMMIT}}) {{DATE}}'
RELEASE_CGO_ENABLED=0
RELEASE_INCLUDE_LICENSE=1
CLI_TEMPLATE_FINGERPRINT="816f217b3a5c95477b24e3fb8ba1f3db5470654441b71b16e5385c9cb5b73290"
RELEASE_GO_TOOLCHAIN="go1.27.1"
