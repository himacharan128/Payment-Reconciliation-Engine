package processor

import (
	"fmt"
	"testing"
	"time"
)

// TestDebugNameExtraction traces through name extraction with realistic bank descriptions
func TestDebugNameExtraction(t *testing.T) {
	testCases := []struct {
		description string
		expected    string
	}{
		{"DEPOSIT S ADAMS", ""},           // What do we get?
		{"CHK DEP SMITH JOHN", ""},
		{"SMITH JOHN CHK DEP", ""},
		{"ONLINE PMT SARAH ADAMS", ""},
		{"ACH TRANSFER S ADAMS", ""},
		{"DEP REF 12345 ADAMS", ""},
		{"WIRE TRANSFER JOHN D SMITH", ""},
		{"POS PURCHASE JONES", ""},
		{"MOBILE DEP SMITH J", ""},
		{"CHECK 1234 ADAMS SARAH", ""},
	}

	fmt.Println("\n=== NAME EXTRACTION DEBUG ===")
	for _, tc := range testCases {
		extracted := extractNameFromDescription(tc.description)
		fmt.Printf("Input: %-35s → Extracted: %q\n", tc.description, extracted)
	}
}

// TestDebugJaroWinkler verifies enhanced Jaro-Winkler against known values
func TestDebugJaroWinkler(t *testing.T) {
	testCases := []struct {
		s1       string
		s2       string
		minScore float64 // Minimum expected score
		maxScore float64 // Maximum expected score
	}{
		// Exact matches
		{"JOHN SMITH", "JOHN SMITH", 99.0, 100.0},
		{"SARAH ADAMS", "SARAH ADAMS", 99.0, 100.0},
		
		// Name order swaps (common in bank descriptions) - enhanced algorithm handles these!
		{"SMITH JOHN", "JOHN SMITH", 99.0, 100.0},        // Token-sorted matching
		{"ADAMS SARAH", "SARAH ADAMS", 99.0, 100.0},
		
		// Abbreviations/initials - enhanced algorithm boosts these
		{"S ADAMS", "SARAH ADAMS", 90.0, 100.0},          // Initial + last name
		{"J SMITH", "JOHN SMITH", 90.0, 100.0},
		{"JOHN S", "JOHN SMITH", 90.0, 100.0},
		
		// Partial matches - token overlap scoring
		{"SMITH", "JOHN SMITH", 85.0, 95.0},             // Last name only
		{"ADAMS", "SARAH ADAMS", 85.0, 95.0},
		
		// No match
		{"JONES", "SMITH", 0.0, 30.0},
		{"COMPLETELY DIFFERENT", "JOHN SMITH", 20.0, 50.0},
	}

	fmt.Println("\n=== JARO-WINKLER DEBUG ===")
	fmt.Println("Testing Jaro-Winkler scores (need high scores for name swaps to hit ≥95):")
	fmt.Println()
	
	for _, tc := range testCases {
		score := jaroWinkler(tc.s1, tc.s2)
		status := "✓"
		if score < tc.minScore || score > tc.maxScore {
			status = "✗ UNEXPECTED"
		}
		fmt.Printf("JW(%q, %q) = %.2f%% [expected %.0f-%.0f] %s\n", 
			tc.s1, tc.s2, score, tc.minScore, tc.maxScore, status)
	}
}

// TestDebugFullMatchingPipeline traces through the complete matching pipeline
func TestDebugFullMatchingPipeline(t *testing.T) {
	// Create test candidates that simulate real invoice data
	candidates := []*InvoiceCandidate{
		{
			ID:             "inv-sarah-adams",
			InvoiceNumber:  "INV-2024-001",
			Amount:         "450.00",
			DueDate:        time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC),
			CustomerName:   "Sarah Adams",
			NormalizedName: "SARAH ADAMS",
			Status:         "sent",
		},
		{
			ID:             "inv-john-smith",
			InvoiceNumber:  "INV-2024-002",
			Amount:         "450.00",
			DueDate:        time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC),
			CustomerName:   "John D. Smith",
			NormalizedName: "JOHN D SMITH",
			Status:         "sent",
		},
	}

	testCases := []struct {
		description     string
		amount          string
		txnDate         time.Time
		expectedInvoice string
		minScore        float64
	}{
		// Exact name match - should be auto_matched (≥95)
		{"SARAH ADAMS", "450.00", time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), "inv-sarah-adams", 95.0},
		
		// Name order swap - should still be high
		{"ADAMS SARAH", "450.00", time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), "inv-sarah-adams", 85.0},
		
		// Initial + last name (bank style)
		{"DEPOSIT S ADAMS", "450.00", time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), "inv-sarah-adams", 80.0},
		
		// With noise tokens
		{"CHK DEP SARAH ADAMS", "450.00", time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), "inv-sarah-adams", 90.0},
		
		// Smith with noise
		{"SMITH JOHN DEP", "450.00", time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC), "inv-john-smith", 80.0},
	}

	fmt.Println("\n=== FULL MATCHING PIPELINE DEBUG ===")
	fmt.Printf("\nCandidates:\n")
	for _, c := range candidates {
		fmt.Printf("  - %s: %s (%s) due=%s\n", c.ID, c.CustomerName, c.NormalizedName, c.DueDate.Format("2006-01-02"))
	}
	fmt.Println()
	
	for _, tc := range testCases {
		result := MatchTransaction(tc.description, tc.amount, tc.txnDate, candidates)
		
		extractedName := extractNameFromDescription(tc.description)
		
		matchedID := "none"
		if result.InvoiceID != nil {
			matchedID = *result.InvoiceID
		}
		
		status := "✓"
		if result.Confidence < tc.minScore {
			status = fmt.Sprintf("✗ EXPECTED ≥%.0f", tc.minScore)
		}
		
		fmt.Printf("\nDescription: %q\n", tc.description)
		fmt.Printf("  Extracted name: %q\n", extractedName)
		fmt.Printf("  Score: %.2f%% → Status: %s\n", result.Confidence, result.Status)
		fmt.Printf("  Matched: %s (expected: %s) %s\n", matchedID, tc.expectedInvoice, status)
		
		// Show match details
		if details, ok := result.MatchDetails["name"].(map[string]interface{}); ok {
			fmt.Printf("  Name similarity: %.2f%%\n", details["similarity"])
		}
		if details, ok := result.MatchDetails["date"].(map[string]interface{}); ok {
			fmt.Printf("  Date adjustment: %.1f (delta: %d days)\n", details["adjustment"], details["deltaDays"])
		}
		if details, ok := result.MatchDetails["ambiguity"].(map[string]interface{}); ok {
			fmt.Printf("  Ambiguity: %d candidates, penalty: %.1f\n", details["candidateCount"], details["penalty"])
		}
	}
}

// TestDebugScoreCalculation shows how scores are calculated
func TestDebugScoreCalculation(t *testing.T) {
	fmt.Println("\n=== SCORE CALCULATION DEBUG ===")
	fmt.Println("\nTo achieve ≥95% for auto_matched with BRD thresholds:")
	fmt.Println("  finalScore = nameScore + dateAdjustment - ambiguityPenalty")
	fmt.Println("  Need: finalScore ≥ 95.0")
	fmt.Println()
	fmt.Println("Max possible score components:")
	fmt.Println("  - Name similarity (Jaro-Winkler): 0-100")
	fmt.Println("  - Date adjustment: -10 to +5")
	fmt.Println("  - Ambiguity penalty: 0 to (candidates-2)*1.5")
	fmt.Println()
	
	// Calculate what's needed
	fmt.Println("Scenarios to achieve ≥95%:")
	fmt.Println()
	
	scenarios := []struct {
		nameScore  float64
		dateAdj    float64
		candidates int
		desc       string
	}{
		{100.0, 5.0, 1, "Perfect name + early payment + 1 candidate"},
		{100.0, 2.0, 1, "Perfect name + on-time + 1 candidate"},
		{100.0, 0.0, 1, "Perfect name + late (8-30 days) + 1 candidate"},
		{95.0, 5.0, 1, "95% name + early payment + 1 candidate"},
		{93.0, 2.0, 1, "93% name + on-time + 1 candidate"},
		{90.0, 5.0, 1, "90% name + early + 1 candidate"},
		{100.0, 5.0, 2, "Perfect + early + 2 candidates (no penalty)"},
		{100.0, 5.0, 3, "Perfect + early + 3 candidates"},
		{100.0, 5.0, 4, "Perfect + early + 4 candidates"},
	}
	
	for _, s := range scenarios {
		penalty := 0.0
		if s.candidates > 2 {
			penalty = float64(s.candidates-2) * 1.5
		}
		final := s.nameScore + s.dateAdj - penalty
		if final > 100 {
			final = 100
		}
		
		status := "needs_review"
		if final >= 95 {
			status = "auto_matched ✓"
		} else if final >= 60 {
			status = "needs_review"
		} else {
			status = "unmatched"
		}
		
		fmt.Printf("  %.0f + %.0f - %.1f = %.1f → %s (%s)\n", 
			s.nameScore, s.dateAdj, penalty, final, status, s.desc)
	}
}

// TestDebugRealWorldScenarios tests with actual bank transaction patterns
func TestDebugRealWorldScenarios(t *testing.T) {
	fmt.Println("\n=== REAL WORLD BANK DESCRIPTION PATTERNS ===")
	
	// Single candidate (no ambiguity)
	singleCandidate := []*InvoiceCandidate{
		{
			ID:             "inv-1",
			InvoiceNumber:  "INV-001",
			Amount:         "1250.00",
			DueDate:        time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC),
			CustomerName:   "Sarah Adams",
			NormalizedName: "SARAH ADAMS",
			Status:         "sent",
		},
	}
	
	bankDescriptions := []string{
		"SARAH ADAMS",                    // Perfect
		"ADAMS SARAH",                    // Swapped
		"S ADAMS",                        // Initial
		"SARAH A",                        // Last initial
		"DEPOSIT SARAH ADAMS",            // With noise
		"CHK DEP S ADAMS",                // Noise + initial
		"ONLINE PMT ADAMS S",             // Noise + swapped initial
		"ACH SARAH ADAMS PAYMENT",        // Noise around name
		"WIRE FROM ADAMS",                // Just last name + noise
	}
	
	// Transaction on due date (+2 adjustment)
	txnDate := time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC)
	
	fmt.Println("\nSingle candidate (Sarah Adams), transaction on due date:")
	fmt.Println("Target: ≥95 for auto_matched, ≥60 for needs_review")
	fmt.Println()
	
	autoCount := 0
	reviewCount := 0
	unmatchedCount := 0
	
	for _, desc := range bankDescriptions {
		result := MatchTransaction(desc, "1250.00", txnDate, singleCandidate)
		extracted := extractNameFromDescription(desc)
		
		var nameScore float64
		if details, ok := result.MatchDetails["name"].(map[string]interface{}); ok {
			nameScore = details["similarity"].(float64)
		}
		
		icon := "❌"
		if result.Status == "auto_matched" {
			icon = "✅"
			autoCount++
		} else if result.Status == "needs_review" {
			icon = "⚠️"
			reviewCount++
		} else {
			unmatchedCount++
		}
		
		fmt.Printf("%s %-30s → extracted=%q nameScore=%.1f final=%.1f → %s\n",
			icon, desc, extracted, nameScore, result.Confidence, result.Status)
	}
	
	fmt.Printf("\nSummary: auto=%d, review=%d, unmatched=%d\n", autoCount, reviewCount, unmatchedCount)
	
	// Expected: Most should be auto_matched or needs_review
	if autoCount < 4 {
		t.Errorf("Expected at least 4 auto_matched, got %d", autoCount)
	}
}

