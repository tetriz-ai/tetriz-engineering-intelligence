// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		prs    []PR
		issues []Issue
		ok     bool
	}{
		{"distinct keys", []PR{{Key: "a"}}, []Issue{{Key: "a"}}, true},
		{"pull request with no key", []PR{{}}, nil, false},
		{"pull request twice", []PR{{Key: "a"}, {Key: "a"}}, nil, false},
		{"issue with no key", nil, []Issue{{}}, false},
		{"issue twice", nil, []Issue{{Key: "i"}, {Key: "i"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.prs, tt.issues)
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrInvalidRecord)
			}
		})
	}
}
