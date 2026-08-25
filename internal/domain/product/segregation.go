package product

// Same-bin DOT hazard-class segregation (ADR 0010).
//
// This file derives a 9x9, class-level boolean incompatibility matrix from
// the real US DOT segregation regulation, 49 CFR §177.848 ("Segregation of
// hazardous materials"), and exposes Incompatible(a, b) so StowStock can
// reject stowing a hazmat-classified SKU into a bin that already holds an
// incompatible hazmat-classified SKU.
//
// ------------------------------------------------------------------------
// Grounding: what 49 CFR §177.848 actually says
// ------------------------------------------------------------------------
//
// §177.848(d)'s "Segregation Table for Hazardous Materials" is a
// division/zone-level table (explosives 1.1-1.6, gases 2.1/2.2/2.3 zone
// A/B, flammable liquids 3, flammable/combustible/water-reactive solids
// 4.1/4.2/4.3, oxidizers/organic peroxides 5.1/5.2, poison liquids 6.1,
// radioactive 7, corrosive liquids 8) whose cells hold one of:
//
//   - "X" — must NOT be loaded, transported, or stored together at all;
//   - "O" — must be kept separated by distance (physical separation
//     sufficient that a leak from one would not commingle with the
//     other), NOT an absolute ban on sharing the same vehicle/facility;
//   - blank — no restriction.
//
// Class 1 (explosives) divisions are further subject to a SEPARATE
// compatibility-group table at §177.848(f) (groups A-L/S), layered on top
// of the division-level table.
//
// ------------------------------------------------------------------------
// Simplification rules for this study project (deliberate, documented)
// ------------------------------------------------------------------------
//
// This service models a discrete warehouse BIN, not a transport vehicle
// with distance-separated compartments, and this platform's
// ProductClassification taxonomy (ADR 0009) does not track division/zone
// granularity — only the top-level DOT hazard class (1-9). Four rules
// bridge that gap, each a conscious loss of precision relative to the real
// regulation, not an oversight:
//
//  1. COLLAPSE TO CLASS. Every division/zone row and column in §177.848(d)
//     is collapsed to its parent top-level class (e.g. 2.1/2.2/2.3A/2.3B
//     all become "class 2"; 4.1/4.2/4.3 all become "class 4"). This turns
//     the division-level table into a 9x9 class-level matrix. When
//     divisions within the same collapsed class disagree with each other
//     (e.g. 2.1-vs-2.3A is "X" while 2.1-vs-2.3B is "O"), rule 4 below
//     picks the most conservative entry, so information is only ever
//     lost in the safe direction.
//  2. X AND O BOTH BLOCK CO-STORAGE. A discrete bin cannot express "keep
//     these two pallets apart by a safe distance within the same 10-foot
//     slot" — there is no partial/graduated storage relationship in this
//     domain, only "in this bin" or not. So both "X" and "O" from the
//     real table are treated as "incompatible for same-bin storage": "O"
//     is upgraded to a hard block rather than downgraded to no
//     restriction. This is conservative by construction — the real
//     regulation never requires MORE separation than an "O" would.
//  3. CLASS 1 IS MAXIMALLY RESTRICTIVE. §177.848(f)'s compatibility-group
//     table (groups A-L/S) governs which explosives may share a vehicle,
//     and even lets some same-division explosives coexist (e.g. group A
//     is incompatible with every other group, including some other group
//     A material is fine, but groups differ across the table in complex,
//     conditional ways: "2" merges C/D/E into E, "3" merges C/D/E+N into
//     D, etc.). Modeling that here would require this service to track
//     compatibility groups A-L/S per SKU, which ADR 0009's HandlingTag
//     taxonomy does not carry and which the business explicitly scoped
//     OUT for this round (see the task brief). Instead, class 1 is
//     modeled as incompatible with EVERY class, including a second class
//     1 SKU of unknown/different nature — the single safest simplification
//     available without that data. A warehouse that actually stores
//     multiple explosives divisions/compatibility-groups in the same
//     facility needs the real §177.848(f) table, not this matrix.
//  4. MOST-CONSERVATIVE-ENTRY-WINS. For classes 2-8, wherever the
//     collapse in rule 1 merges multiple real table entries that
//     disagree (some blank, some "O", some "X"), the most restrictive
//     value present anywhere in that collapsed cell decides the
//     class-level entry: "X" beats "O" beats blank. This is why, for
//     example, class 2 (gases) is derived as self-incompatible even
//     though 2.2 (non-flammable, non-toxic gas) has no restriction
//     against most things — 2.1 (flammable) vs 2.3A (poison gas Zone A)
//     is "X" in the real table, and that division pair collapses into
//     the same "class 2 vs class 2" cell.
//
// Class 9 (miscellaneous dangerous goods) does not appear in §177.848(d)'s
// table at all — it was added to the HMR after this table's structure was
// set, and is commercially treated (by carriers, freight forwarders, and
// DOT guidance materials) as broadly compatible with everything except
// Class 1 explosives. That commercial convention, not a table cell, is
// what backs class 9's row/column below.
//
// ------------------------------------------------------------------------
// The derived 9x9 matrix
// ------------------------------------------------------------------------
//
// X = incompatible for same-bin storage under the rules above.
// . = no restriction (compatible).
//
//	     1  2  3  4  5  6  7  8  9
//	1:   X  X  X  X  X  X  X  X  X    Explosives (rule 3: max-restrictive)
//	2:   X  X  X  X  X  X  X  X  .    Gases (2.1/2.2/2.3A/2.3B)
//	3:   X  X  .  .  X  X  .  .  .    Flammable liquids
//	4:   X  X  .  .  .  X  .  X  .    Flammable/combustible/water-reactive solids
//	5:   X  X  X  .  .  X  .  X  .    Oxidizers, organic peroxides
//	6:   X  X  X  X  X  .  .  X  .    Poison (toxic) liquids
//	7:   X  X  .  .  .  .  .  .  .    Radioactive
//	8:   X  X  .  X  X  X  .  .  .    Corrosives
//	9:   X  .  .  .  .  .  .  .  .    Miscellaneous dangerous goods (rule: compatible w/ all but class 1)
//
// Derivation trace (which real §177.848(d) division cells produced each
// class-2..8 "X" above, after rules 1/2/4 — for auditability):
//
//	2x2: 2.1/2.3A="X" (2.1/2.3B="O" agrees after rule 2)
//	2x3: 2.3A/3="X"          2x4: 2.3A/4.1|4.2|4.3="X"     2x5: 2.3A/5.1|5.2="X"
//	2x6: 2.1/6.1="O"         2x7: 2.1/7="O"                2x8: 2.3A/8="X"
//	3x5: 3/5.1="O"           3x6: 3/6.1="X"
//	4x6: 4.1|4.2|4.3/6.1="X" 4x8: 4.2/8="X" (4.1/8="O", 4.3/8="O" agree after rule 2)
//	5x6: 5.1|5.2/6.1="X"     5x8: 5.1|5.2/8="O"
//	6x8: 6.1/8="X"
//
// Cells not listed above (3x4, 3x7, 3x8, 4x5, 4x7, 5x7, 6x7, 7x8) have no
// "X" or "O" entry anywhere in the collapsed real table and are compatible.
type segregationMatrix [10][10]bool

// classIncompatibility is the derived matrix. Indices 1-9 are used;
// index 0 is unused padding so DOTHazardClass values index directly.
// Populated once at init time from the symmetric pair list below, so the
// literal only needs to state each incompatible pair once.
var classIncompatibility = buildSegregationMatrix()

// incompatiblePairs enumerates every incompatible (class, class) pair in
// the derived matrix above, upper-triangle only (a <= b); buildSegregationMatrix
// mirrors each pair across the diagonal. This is the single source of
// truth the doc comment's ASCII table is a rendering of.
var incompatiblePairs = [][2]int{
	// Class 1: incompatible with every class, including itself (rule 3).
	{1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}, {1, 6}, {1, 7}, {1, 8}, {1, 9},
	// Class 2 (gases): self-incompatible, incompatible with 3-8, compatible with 9.
	{2, 2}, {2, 3}, {2, 4}, {2, 5}, {2, 6}, {2, 7}, {2, 8},
	// Class 3 (flammable liquids).
	{3, 5}, {3, 6},
	// Class 4 (flammable/combustible/water-reactive solids).
	{4, 6}, {4, 8},
	// Class 5 (oxidizers, organic peroxides).
	{5, 6}, {5, 8},
	// Class 6 (poison liquids).
	{6, 8},
	// Class 7 (radioactive): no cross-class "X"/"O" beyond class 1/2 above.
	// Class 8 (corrosives): remaining pairs already listed above.
	// Class 9: compatible with everything except class 1 (already listed).
}

func buildSegregationMatrix() segregationMatrix {
	var m segregationMatrix
	for _, pair := range incompatiblePairs {
		a, b := pair[0], pair[1]
		m[a][b] = true
		m[b][a] = true
	}
	return m
}

// Incompatible reports whether two DOT hazard classes must not share the
// same bin under the derived segregation matrix above.
//
// Fail-open, consistent with the rest of this service's classification
// design: DOTHazardClassUnspecified (the zero value) is always compatible
// with anything, including another unspecified class. StowStock therefore
// never blocks a stow on account of a bin occupant, or the SKU being
// stowed, that carries no DOT hazard class at all — only SKUs that both
// carry an actual class (1-9) can be found incompatible.
func Incompatible(a, b DOTHazardClass) bool {
	if a == DOTHazardClassUnspecified || b == DOTHazardClassUnspecified {
		return false
	}
	if a < 1 || a > 9 || b < 1 || b > 9 {
		// Defensive: should be unreachable for any DOTHazardClass that
		// passed ParseDOTHazardClass, but never index out of range.
		return false
	}
	return classIncompatibility[a][b]
}
