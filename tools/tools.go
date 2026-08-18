// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build tools
// +build tools

package tools

// This file pins developer tooling as module dependencies.
// See: https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

import (
    _ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)