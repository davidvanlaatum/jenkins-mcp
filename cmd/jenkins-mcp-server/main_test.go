package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasSelfUpdateFlagRecognizesAnySelfUpdateForm(t *testing.T) {
	r := require.New(t)

	for _, args := range [][]string{
		{"--self-update"},
		{"-self-update"},
		{"--self-update=true"},
		{"-self-update=true"},
		{"--self-update=1"},
		{"--self-update=t"},
		{"--self-update=false"},
		{"-self-update=false"},
		{"--self-update=0"},
		{"--self-update=not-bool"},
	} {
		r.True(hasSelfUpdateFlag(args), "hasSelfUpdateFlag(%v)", args)
	}
}

func TestHasSelfUpdateFlagIgnoresUnrelatedFlags(t *testing.T) {
	r := require.New(t)

	for _, args := range [][]string{
		{"--other"},
	} {
		r.False(hasSelfUpdateFlag(args), "hasSelfUpdateFlag(%v)", args)
	}
}

func TestStripSelfUpdateFlagsRemovesSelfUpdateModeFlags(t *testing.T) {
	r := require.New(t)

	got := stripSelfUpdateFlags([]string{"--self-update=false", "--force=true", "--config", "config.json", "--other"})
	r.Equal([]string{"--config", "config.json", "--other"}, got, "stripSelfUpdateFlags()")
}
