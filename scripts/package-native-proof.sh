#!/usr/bin/env bash
set -euo pipefail
# A separate experimental archive; production release packaging is unchanged.
if [[ $# -ne 2 || "$1" != "--ad-hoc" || "$2" != /* ]]; then
  echo 'usage: scripts/package-native-proof.sh --ad-hoc /absolute/new-output-directory' >&2
  echo 'Homebrew proof: ad-hoc signing, no notarization. Stable deployment qualification remains required.' >&2
  exit 2
fi
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="$2"
if [[ -e "$OUTPUT" ]]; then echo 'output directory must not already exist' >&2; exit 2; fi
mkdir -p "$OUTPUT"
read -r BUILD_VERSION BUILD_COMMIT BUILD_DATE < <(python3 - "$ROOT_DIR" "$OUTPUT" <<'BUILDINFO'
import datetime, hashlib, json, pathlib, subprocess, sys
root, output = map(pathlib.Path, sys.argv[1:])
files = [root / "go.mod", root / "go.sum"]
for directory, suffix in [("cmd", ".go"), ("internal", ".go"), ("native", ".swift")]:
    files.extend((root / directory).rglob("*" + suffix))
files.extend(root / "scripts" / name for name in
             ["package-native-proof.sh", "test-native-proof.sh", "native-proof-smoke.py", "native-proof-formula.py"])
sources = {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
           for p in sorted(files)}
source_hash = hashlib.sha256(json.dumps(sources, sort_keys=True).encode()).hexdigest()
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
dirty = bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=root, text=True))
built_at = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
version = "native-proof." + source_hash[:12]
commit = revision + ("-dirty" if dirty else "")
info = dict(version=version, revision=revision, dirty=dirty, built_at=built_at,
            source_sha256=source_hash, sources=sources, signing="ad-hoc",
            protocol="acal-native-proof-v1",
            go=subprocess.check_output(["go", "version"], text=True).strip(),
            swift=subprocess.check_output(["xcrun", "swiftc", "--version"], text=True).strip())
(output / "BUILD-INFO.json").write_text(json.dumps(info, indent=2) + "\n")
print(version, commit, built_at)
BUILDINFO
)
[[ -n "$BUILD_VERSION" && -n "$BUILD_COMMIT" && -n "$BUILD_DATE" ]]
for arch in arm64 amd64; do
  target="$arch"; [[ "$arch" == amd64 ]] && target=x86_64
  package="$OUTPUT/acal-native-proof-darwin-$arch"
  bundle="$package/libexec/acal-native.app"
  mkdir -p "$package/bin" "$bundle/Contents/MacOS"
  (cd "$ROOT_DIR" && GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$BUILD_VERSION -X main.commit=$BUILD_COMMIT -X main.date=$BUILD_DATE" -o "$package/bin/acal" ./cmd/acal)
  xcrun swiftc -swift-version 5 -O -target "$target-apple-macos14.0" -framework EventKit "$ROOT_DIR/native/EventKitHelper/Core.swift" "$ROOT_DIR/native/EventKitHelper/main.swift" -o "$bundle/Contents/MacOS/acal-native"
  cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.agisilaos.acal.native-proof</string>
<key>CFBundleExecutable</key><string>acal-native</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleName</key><string>acal native proof</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>LSUIElement</key><true/>
<key>NSCalendarsFullAccessUsageDescription</key><string>acal's experimental native proof reads Calendar and changes only its own disposable fixture events.</string>
</dict></plist>
PLIST
  python3 - "$bundle/Contents/Info.plist" "$BUILD_VERSION" "$BUILD_DATE" <<'BUNDLEINFO'
import pathlib, plistlib, sys
path = pathlib.Path(sys.argv[1])
info = plistlib.loads(path.read_bytes())
info["CFBundleVersion"] = "".join(c for c in sys.argv[3] if c.isdigit())
info["ACALBuildVersion"] = sys.argv[2]
info["ACALBuildDate"] = sys.argv[3]
path.write_bytes(plistlib.dumps(info))
BUNDLEINFO
  codesign --force --sign - "$bundle"
  codesign --force --sign - "$package/bin/acal"
  codesign --verify --strict "$bundle"
  plutil -lint "$bundle/Contents/Info.plist"
  cp "$ROOT_DIR/LICENSE" "$package/LICENSE"
  cp "$OUTPUT/BUILD-INFO.json" "$package/BUILD-INFO.json"
  printf '%s\n' 'EXPERIMENTAL: ad-hoc signed; not Developer ID signed or notarized; not qualified for production.' > "$package/QUALIFICATION.txt"
  tar -czf "$OUTPUT/acal-native-proof-darwin-$arch.tar.gz" -C "$package" .
done
(cd "$OUTPUT" && shasum -a 256 ./*.tar.gz > SHA256SUMS)
printf 'Development proof archives: %s\n' "$OUTPUT"
