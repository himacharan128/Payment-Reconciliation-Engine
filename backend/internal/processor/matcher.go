package processor

import (
	"math"
	"sort"
	"strings"
	"time"
)

type MatchResult struct {
	InvoiceID      *string
	Confidence     float64
	Status         string // auto_matched, needs_review, unmatched
	MatchDetails   map[string]interface{}
}

type scoredCandidate struct {
	candidate        *InvoiceCandidate
	nameScore        float64
	dateDelta        int
	dateAdjustment   float64
	ambiguityPenalty float64
	finalScore       float64
}

// MatchTransaction matches a bank transaction against invoice candidates
func MatchTransaction(
	description string,
	amount string,
	transactionDate time.Time,
	candidates []*InvoiceCandidate,
) MatchResult {
	if len(candidates) == 0 {
		return MatchResult{
			Confidence: 0,
			Status:     "unmatched",
			MatchDetails: map[string]interface{}{
				"version": "v1",
				"reason":  "no_invoice_with_matching_amount",
			},
		}
	}

	// Extract and normalize name from description
	extractedName := extractNameFromDescription(description)
	
	// If name extraction is too weak, cap confidence
	nameTooWeak := len(extractedName) < 3 || strings.TrimSpace(extractedName) == ""
	
	// Score each candidate
	scored := make([]scoredCandidate, 0, len(candidates))
	
	for _, cand := range candidates {
		// Name similarity (primary factor, 0-100)
		nameScore := jaroWinkler(extractedName, cand.NormalizedName)
		
		// If name extraction is weak, cap name score contribution
		if nameTooWeak {
			nameScore = math.Min(nameScore, 50.0) // Cap at 50 if name extraction failed
		}
		
		// Date proximity adjustment (-10 to +5 points)
		dateDelta := int(transactionDate.Sub(cand.DueDate).Hours() / 24)
		dateAdjustment := calculateDateAdjustment(dateDelta)
		
		// Ambiguity penalty (if multiple candidates)
		ambiguityPenalty := 0.0
		if len(candidates) > 1 {
			ambiguityPenalty = float64(len(candidates)-1) * 2.0 // -2 points per extra candidate
		}
		
		// Final score: nameScore + dateAdjustment - ambiguityPenalty
		finalScore := nameScore + dateAdjustment - ambiguityPenalty
		finalScore = math.Max(0, math.Min(100, finalScore)) // Clamp 0-100
		finalScore = math.Round(finalScore*100) / 100 // Round to 2 decimals
		
		scored = append(scored, scoredCandidate{
			candidate: cand,
			nameScore: nameScore,
			dateDelta: dateDelta,
			dateAdjustment: dateAdjustment,
			ambiguityPenalty: ambiguityPenalty,
			finalScore: finalScore,
		})
	}
	
	// Sort by score descending, then by tie-breakers
	sort.Slice(scored, func(i, j int) bool {
		if math.Abs(scored[i].finalScore - scored[j].finalScore) < 0.01 { // Within epsilon
			// Tie-breaker 1: smaller absolute date delta
			absDeltaI := math.Abs(float64(scored[i].dateDelta))
			absDeltaJ := math.Abs(float64(scored[j].dateDelta))
			if absDeltaI != absDeltaJ {
				return absDeltaI < absDeltaJ
			}
			// Tie-breaker 2: earlier due date
			return scored[i].candidate.DueDate.Before(scored[j].candidate.DueDate)
		}
		return scored[i].finalScore > scored[j].finalScore
	})
	
	best := scored[0]
	
	// Determine status bucket (exact thresholds)
	status := "unmatched"
	if best.finalScore >= 95.0 {
		status = "auto_matched"
	} else if best.finalScore >= 60.0 {
		status = "needs_review"
	}
	
	// If multiple candidates and name is weak, don't auto-match
	if status == "auto_matched" && len(candidates) > 1 && nameTooWeak {
		status = "needs_review"
	}
	
	// Build match details with stable schema
	matchDetails := buildMatchDetails(
		description,
		amount,
		transactionDate,
		best.candidate,
		scored,
		best.nameScore,
		best.dateDelta,
		best.dateAdjustment,
		best.ambiguityPenalty,
		best.finalScore,
		status,
	)
	
	var invoiceID *string
	if status != "unmatched" {
		invoiceID = &best.candidate.ID
	}
	
	return MatchResult{
		InvoiceID:    invoiceID,
		Confidence:   best.finalScore,
		Status:       status,
		MatchDetails: matchDetails,
	}
}

func buildMatchDetails(
	description string,
	amount string,
	transactionDate time.Time,
	bestCandidate *InvoiceCandidate,
	allScored []scoredCandidate,
	nameScore float64,
	dateDelta int,
	dateAdjustment float64,
	ambiguityPenalty float64,
	finalScore float64,
	bucket string,
) map[string]interface{} {
	extractedName := extractNameFromDescription(description)
	
	details := map[string]interface{}{
		"version": "v1",
		"amount": map[string]interface{}{
			"transaction": amount,
			"invoice":     nil,
		},
		"name": map[string]interface{}{
			"extracted":   extractedName,
			"invoiceName": nil,
			"similarity":  nameScore,
		},
		"date": map[string]interface{}{
			"transactionDate": transactionDate.Format("2006-01-02"),
			"invoiceDueDate": nil,
			"deltaDays":      dateDelta,
			"adjustment":     dateAdjustment,
		},
		"ambiguity": map[string]interface{}{
			"candidateCount": len(allScored),
			"penalty":        ambiguityPenalty,
		},
		"finalScore": finalScore,
		"bucket":     bucket,
		"topCandidates": []interface{}{},
	}
	
	if bestCandidate != nil {
		details["amount"].(map[string]interface{})["invoice"] = bestCandidate.Amount
		details["name"].(map[string]interface{})["invoiceName"] = bestCandidate.CustomerName
		details["date"].(map[string]interface{})["invoiceDueDate"] = bestCandidate.DueDate.Format("2006-01-02")
	}
	
	// Build top candidates (up to 3)
	topCandidates := make([]interface{}, 0, 3)
	for i := 0; i < len(allScored) && i < 3; i++ {
		topCandidates = append(topCandidates, map[string]interface{}{
			"invoiceId":     allScored[i].candidate.ID,
			"invoiceNumber": allScored[i].candidate.InvoiceNumber,
			"score":         allScored[i].finalScore,
			"nameScore":     allScored[i].nameScore,
			"deltaDays":     allScored[i].dateDelta,
		})
	}
	details["topCandidates"] = topCandidates
	
	return details
}

func extractNameFromDescription(desc string) string {
	// Remove common noise tokens
	noiseTokens := []string{
		"CHK", "DEP", "PMT", "PAYMENT", "ONLINE", "TRANSFER", "ACH",
		"DEPOSIT", "WIRE", "CHECK", "REF", "REFERENCE", "MISC",
	}
	
	desc = strings.ToUpper(desc)
	
	// Remove noise tokens
	for _, token := range noiseTokens {
		desc = strings.ReplaceAll(desc, token, " ")
	}
	
	// Normalize
	return normalizeName(desc)
}

func calculateDateAdjustment(daysDelta int) float64 {
	// Transaction before due date: +5 points
	if daysDelta < 0 {
		return 5.0
	}
	// Transaction on or near due date (0-7 days): +2 points
	if daysDelta <= 7 {
		return 2.0
	}
	// Transaction 8-30 days after: 0 points
	if daysDelta <= 30 {
		return 0.0
	}
	// Transaction >30 days after: -10 points
	return -10.0
}

// Jaro-Winkler similarity (simplified version)
func jaroWinkler(s1, s2 string) float64 {
	if s1 == s2 {
		return 100.0
	}
	
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}
	
	// Simple character-based similarity
	// Count matching characters (order-independent)
	matches := 0
	s1Chars := make(map[rune]int)
	s2Chars := make(map[rune]int)
	
	for _, c := range s1 {
		s1Chars[c]++
	}
	for _, c := range s2 {
		s2Chars[c]++
	}
	
	for c, count1 := range s1Chars {
		if count2, exists := s2Chars[c]; exists {
			matches += int(math.Min(float64(count1), float64(count2)))
		}
	}
	
	if matches == 0 {
		return 0.0
	}
	
	// Jaro similarity
	jaro := float64(matches) / math.Max(float64(len(s1)), float64(len(s2)))
	
	// Winkler prefix bonus (first 4 chars)
	prefixLen := 0
	maxPrefix := int(math.Min(4, math.Min(float64(len(s1)), float64(len(s2)))))
	for i := 0; i < maxPrefix && i < len(s1) && i < len(s2); i++ {
		if strings.ToUpper(string(s1[i])) == strings.ToUpper(string(s2[i])) {
			prefixLen++
		} else {
			break
		}
	}
	
	winkler := jaro + (0.1 * float64(prefixLen) * (1 - jaro))
	
	return winkler * 100.0
}
