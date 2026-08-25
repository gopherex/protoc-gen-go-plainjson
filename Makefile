SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c

MODULE := github.com/gopherex/protoc-gen-go-plainjson
# v2+ needs semantic import versioning (/vN in the module path), unsupported
# here — keep releases on v0/v1.
MAX_MAJOR := 1
SPEC_DIR := $(CURDIR)/example/spec
INVALID_DIR := $(CURDIR)/testdata/invalid

.PHONY: help build gen gen-opts gen-invalid test bench tidy release

help:
	@echo "make build       - build bin/protoc-gen-go-plainjson"
	@echo "make gen-opts    - regenerate plainjson/plainjson.pb.go from the option proto"
	@echo "make gen         - regenerate the spec protos and the descriptor fixture"
	@echo "make gen-invalid - recompile testdata/invalid into its descriptor fixture"
	@echo "make test        - gofmt + go vet + go test (like CI)"
	@echo "make bench       - run the benchmarks in example/bench"
	@echo "make tidy        - go mod tidy"
	@echo "make release     - interactive tag + push (vX.Y.Z); triggers the Release workflow"

build:
	go build -o $(CURDIR)/bin/protoc-gen-go-plainjson ./

# The option proto, the spec protos and the benchmark corpus share one easyp
# run. The descriptor set it writes is the fixture the generation tests drive
# the plugin with, so no protoc is needed at test time.
gen gen-opts: build
	easyp generate --descriptor_set_out $(CURDIR)/testdata/spec-descriptors.binpb --include_imports

# Protos that must be rejected. Compiled to a descriptor set only: no code is
# generated from them.
gen-invalid:
	cd $(INVALID_DIR) && easyp generate --root $(CURDIR) \
		--descriptor_set_out descriptors.binpb --include_imports

test:
	out=$$(gofmt -l .)
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	go test ./...

bench:
	go test ./example/bench/ -bench . -benchmem -benchtime=3s -run XXX

tidy:
	go mod tidy

# Interactive release: recreate the latest tag on HEAD, or bump major/minor/patch.
# Pushing the vX.Y.Z tag triggers .github/workflows/release.yml.
release:
	@cd "$$(git rev-parse --show-toplevel)"
	if [ -n "$$(git status --porcelain)" ]; then
	  echo "✗ Working tree not clean — commit or stash first:"
	  git status --short
	  exit 1
	fi
	cur="$$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)"
	cur="$${cur:-0.0.0}"
	head="$$(git rev-parse --short HEAD)"
	echo "Latest release: v$$cur    HEAD: $$head"
	echo
	echo "  1) recreate v$$cur on HEAD   [force]"
	echo "  2) bump version"
	echo "  3) cancel"
	read -r -p "> " action
	case "$$action" in
	1)
	  if ! git tag -l "v$$cur" | grep -q .; then echo "✗ No release tag to recreate."; exit 1; fi
	  echo "Will DELETE and recreate v$$cur on $$head, then force-push."
	  read -r -p "Type 'yes' to proceed: " ok
	  [ "$$ok" = "yes" ] || { echo "Aborted."; exit 0; }
	  git tag -d "v$$cur" 2>/dev/null || true
	  git push origin ":refs/tags/v$$cur" 2>/dev/null || true
	  git tag -a "v$$cur" -m "v$$cur"
	  git push origin --force "v$$cur"
	  echo "✓ Recreated v$$cur on $$head."
	  ;;
	2)
	  IFS=. read -r MA MI PA <<< "$$cur"
	  echo
	  echo "  1) major  -> v$$((MA+1)).0.0"
	  echo "  2) minor  -> v$$MA.$$((MI+1)).0"
	  echo "  3) patch  -> v$$MA.$$MI.$$((PA+1))"
	  read -r -p "> " comp
	  case "$$comp" in
	    1) MA=$$((MA+1)); MI=0; PA=0 ;;
	    2) MI=$$((MI+1)); PA=0 ;;
	    3) PA=$$((PA+1)) ;;
	    *) echo "Aborted."; exit 0 ;;
	  esac
	  if [ "$$MA" -gt "$(MAX_MAJOR)" ]; then
	    echo "✗ v$$MA needs semantic import versioning (/v$$MA in the module path); stay on v0/v1."
	    exit 1
	  fi
	  new="$$MA.$$MI.$$PA"
	  echo
	  echo "Release v$$new — create tag v$$new on $$head and push."
	  read -r -p "Type 'yes' to proceed: " ok
	  [ "$$ok" = "yes" ] || { echo "Aborted."; exit 0; }
	  git tag -a "v$$new" -m "v$$new"
	  git push origin "v$$new"
	  echo "✓ Released v$$new — the Release workflow will publish it."
	  ;;
	*)
	  echo "Cancelled."
	  ;;
	esac
