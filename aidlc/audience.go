// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

// AudienceProduct is the audience of a constraint about the measurement's own
// instrumentation rather than the organisation's practice: it stays in
// Result.Bottlenecks for whoever runs the measurement and never reaches the profile.
const AudienceProduct = "product"

// AudienceOrg is the audience of a constraint about the organisation's own practice:
// it reaches the profile's bottlenecks.
const AudienceOrg = "org"
