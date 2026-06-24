package rar

import (
	"fmt"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

// The real 63 obfuscated volume filenames from the Supergirl import that
// motivated this fix: one multi-volume RAR set, every volume a distinct random
// base sharing only the .partNN.rar tail.
var supergirlObfuscatedNames = []string{
	"L3A5Gk5sWzcpO6dRRh9c9XOfVDO.part01.rar",
	"dEVjNplTaH_Uyli0lv7iwzqp.part02.rar",
	"dwqjwYy9uDXcBB6Yq77U.part03.rar",
	"v9eQrQMSTksT_KW.part04.rar",
	"UzdC_P-XSgn0zxjNWgCBxMu.part05.rar",
	"5UiylPHkjhquLl-X.part06.rar",
	"hwuiEqkiNcDvsBagMJTKZXZNCxy.part07.rar",
	"cij_8OH4GRxF9wFcAMaVRXPhT.part08.rar",
	"HzR78H-BTCWNAQos8.part09.rar",
	"UlbjH6LXDAjKHSPdUd-v.part10.rar",
	"8wtnCv_2rf5YLIDPDv.part11.rar",
	"513qIlRC5AB1KC9d.part12.rar",
	"Q4I6bwgWUWL1PNctnCPlfoou.part13.rar",
	"NMWAnBYCelmpfTuXgKhTuThVBNXOI.part14.rar",
	"3VyRPUgW0hpPMPMrgUpNvj72NMD.part15.rar",
	"QlMTceOhfxwxOXo8WS3wueozmBBp_.part16.rar",
	"oSTJyDFyxRsLp97J4v7TUMF4bVbV.part17.rar",
	"XgxuiGSvg2PbQjEi9N.part18.rar",
	"qjLYWZY046oYHrMZyurueBKyi9.part19.rar",
	"NHdIG0aOmToeHPiLM3X71IQdYjjBu.part20.rar",
	"yYdnWiTk-3vM1aP_ZfNe-LvjA.part21.rar",
	"yHvbhmhU5l8PBGF_0f-ha2Q.part22.rar",
	"4E570NfUj92vJRMa.part23.rar",
	"qZt8l451hOC6q1mVmaIu-Tk76f.part24.rar",
	"QUSXp9oEDPB-jfmDkXLj05.part25.rar",
	"hY6hAN6WfwV4lXzf1M.part26.rar",
	"pWbQNRgW_cJutY0sB.part27.rar",
	"WkIwxU5v9BJc890.part28.rar",
	"giOOfS-2-LXG7zGL378AlS20GaT-.part29.rar",
	"bntdLuVRscqtaQVpnz9X456o.part30.rar",
	"qJeCl4aVTqKQrAk0zu7MIK5YF2.part31.rar",
	"lJaBCejA5Wnx72JDPtnkWoQ5.part32.rar",
	"8HTLOmEtET6x1iYfs2hfxef0h_I.part33.rar",
	"69y0B9W9O9IvnHidspGQz8.part34.rar",
	"kaIouYfU-5V1bqpakxkCL_BSVhw.part35.rar",
	"7jZx9koqiE1sAlE0.part36.rar",
	"1RbJ9Sbpu3N4VjuKP7viA.part37.rar",
	"uQHYRfN6IyWGMyxnUMw.part38.rar",
	"hc9Iiqze1OCh-w5MXm6hVdNKlch.part39.rar",
	"1cWdeqO_34yy04nuVPimGhQVzttu1.part40.rar",
	"Q7oIYisCBLHReCoKxU2QJAF3uOV.part41.rar",
	"8PcBe3NkRmhzH8JR8SuIAYg.part42.rar",
	"rbDtxFr53aNtolA6.part43.rar",
	"LuXEbAQAqTn7oc4N10.part44.rar",
	"567dEAE4nEhzszXZBz-mK4u54L37.part45.rar",
	"OmZV7RflINJ_wmD_RM6.part46.rar",
	"WPJieG5E2bCI_Yix9BJbLl.part47.rar",
	"VOiBNRNksZa6PQa4nJ.part48.rar",
	"hnWsPCGkpBhwW7ZKAaVO.part49.rar",
	"5VZazJMVS4D80JeMe19QwVrR.part50.rar",
	"V9DuskcVYLXjHWWPkJkryo6nJB.part51.rar",
	"s_Iu4rp6150lW0-KKL0BWuZjLZf.part52.rar",
	"FnCfK_SVUZdLvessJISJw6V.part53.rar",
	"MqBTEfs9-iwr7ra.part54.rar",
	"9KjFsbvoE9THLyuonPbZIfYO8j.part55.rar",
	"ey77la3f8ivtmVt_3TN.part56.rar",
	"tNDfyngMRCBeYzoiHb31Mm.part57.rar",
	"8T1-58mVkESwn6iesDsSofzN-VNp.part58.rar",
	"W7q64eEkiwCVm_6OhZWcLGF.part59.rar",
	"7td-8bmggETPPMRuj4.part60.rar",
	"ba2cnYicTsi9OETwJHvpQlDyr3.part61.rar",
	"6mZCXfIFhD0oWjgMJhJXcUUmDpL.part62.rar",
	"dTg6VhL2GwBJZ2jeEAm5SEA3qLY.part63.rar",
}

// flatFiles builds a parser.ParsedFile list (NzbdavID = filename, so identity
// can be tracked through the rename) for feeding GroupArchivesByBaseName.
func flatFiles(names []string) []parser.ParsedFile {
	out := make([]parser.ParsedFile, 0, len(names))
	for _, n := range names {
		out = append(out, parser.ParsedFile{Filename: n, NzbdavID: n})
	}
	return out
}

// singletonGroups builds one-file-per-group input, the shape
// GroupArchivesByBaseName produces for an obfuscated set, for direct unit tests
// of reconcileObfuscatedPartSet.
func singletonGroups(names []string) [][]parser.ParsedFile {
	groups := make([][]parser.ParsedFile, 0, len(names))
	for _, n := range names {
		groups = append(groups, []parser.ParsedFile{{Filename: n, NzbdavID: n}})
	}
	return groups
}

// TestGroupArchivesByBaseName_ObfuscatedSet exercises the full grouping pipeline
// end to end: 63 distinctly-named volumes must collapse into one followable set.
func TestGroupArchivesByBaseName_ObfuscatedSet(t *testing.T) {
	groups := GroupArchivesByBaseName(flatFiles(supergirlObfuscatedNames))

	if len(groups) != 1 {
		t.Fatalf("expected 1 group after reconcile, got %d", len(groups))
	}
	merged := groups[0]
	if len(merged) != 63 {
		t.Fatalf("expected 63 volumes, got %d", len(merged))
	}

	for i, f := range merged {
		ord := i + 1
		wantName := fmt.Sprintf("%s.part%02d.rar", obfuscatedSetSyntheticBase, ord)
		if f.Filename != wantName {
			t.Errorf("vol %d: filename = %q, want %q", i, f.Filename, wantName)
		}
		// Identity preserved: the volume now named partNN is the original file
		// whose ordinal was NN (NzbdavID still holds the original name).
		wantSuffix := fmt.Sprintf(".part%02d.rar", ord)
		if got := f.NzbdavID; len(got) < len(wantSuffix) || got[len(got)-len(wantSuffix):] != wantSuffix {
			t.Errorf("vol %d (%s): original identity %q lacks ordinal %d", i, f.Filename, f.NzbdavID, ord)
		}
	}
}

// TestReconcileObfuscatedPartSet_Bails verifies every non-trigger shape is left
// exactly as-is, so an import that works today can never be altered.
func TestReconcileObfuscatedPartSet_Bails(t *testing.T) {
	cases := []struct {
		name string
		in   [][]parser.ParsedFile
	}{
		{
			name: "clean shared-base set (single multi-file group)",
			in: [][]parser.ParsedFile{{
				{Filename: "Movie.part01.rar"},
				{Filename: "Movie.part02.rar"},
				{Filename: "Movie.part03.rar"},
			}},
		},
		{
			name: "mixed clean set + obfuscated singletons",
			in: append(
				[][]parser.ParsedFile{{
					{Filename: "Movie.part01.rar"},
					{Filename: "Movie.part02.rar"},
				}},
				singletonGroups(supergirlObfuscatedNames[:5])...,
			),
		},
		{
			name: "duplicate ordinal (two overlapping sets)",
			in: singletonGroups([]string{
				"aaa.part01.rar", "bbb.part02.rar", "ccc.part03.rar",
				"ddd.part01.rar", "eee.part02.rar", "fff.part03.rar",
			}),
		},
		{
			name: "interior gap",
			in:   singletonGroups([]string{"aaa.part01.rar", "bbb.part02.rar", "ddd.part04.rar"}),
		},
		{
			name: "leading gap (does not start at 1)",
			in:   singletonGroups([]string{"aaa.part02.rar", "bbb.part03.rar", "ccc.part04.rar"}),
		},
		{
			name: "below N>=3 floor",
			in:   singletonGroups([]string{"aaa.part01.rar", "bbb.part02.rar"}),
		},
		{
			name: "non-part scheme singletons",
			in:   singletonGroups([]string{"aaa.rar", "aaa.r00", "aaa.r01"}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := reconcileObfuscatedPartSet(tc.in)
			if len(out) != len(tc.in) {
				t.Fatalf("%s: expected unchanged (%d groups), got %d", tc.name, len(tc.in), len(out))
			}
		})
	}
}
