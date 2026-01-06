package processor

import (
	"math"
	"sort"
	"strings"
	"time"
)

type MatchResult struct {
	InvoiceID    *string
	Confidence   float64
	Status       string // auto_matched, needs_review, unmatched
	MatchDetails map[string]interface{}
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
		finalScore = math.Round(finalScore*100) / 100       // Round to 2 decimals

		scored = append(scored, scoredCandidate{
			candidate:        cand,
			nameScore:        nameScore,
			dateDelta:        dateDelta,
			dateAdjustment:   dateAdjustment,
			ambiguityPenalty: ambiguityPenalty,
			finalScore:       finalScore,
		})
	}

	// Sort by score descending, then by tie-breakers
	sort.Slice(scored, func(i, j int) bool {
		if math.Abs(scored[i].finalScore-scored[j].finalScore) < 0.01 { // Within epsilon
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
			"invoiceDueDate":  nil,
			"deltaDays":       dateDelta,
			"adjustment":      dateAdjustment,
		},
		"ambiguity": map[string]interface{}{
			"candidateCount": len(allScored),
			"penalty":        ambiguityPenalty,
		},
		"finalScore":    finalScore,
		"bucket":        bucket,
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
	// Remove common banking noise tokens
	// IMPORTANT: Order by length (longest first) to avoid partial matches
	// e.g., "DEPOSIT" before "DEP" to prevent corruption
	noiseTokens := []string{
		"DEPOSIT", "PAYMENT", "ONLINE", "TRANSFER", "REFERENCE", "MOBILE",
		"WIRE", "CHECK", "DEBIT", "CREDIT", "ATM", "POS", "TXN", "TRANSACTION",
		"WITHDRAWAL", "CARD", "PURCHASE", "AUTHORIZATION", "SETTLEMENT",
		"CHK", "DEP", "PMT", "ACH", "REF", "MISC", "AUTH", "SETTLEMENT",
		"BATCH", "PROCESSING", "FEE", "CHARGE", "ADJUSTMENT", "CLEARING",
		"REVERSAL", "VOID", "RETURN", "DISPUTE", "CHARGEBACK",
	}

	desc = strings.ToUpper(desc)

	// Remove noise tokens using word boundaries (whole words only)
	words := strings.Fields(desc)
	filtered := make([]string, 0, len(words))

	for _, word := range words {
		isNoise := false
		for _, token := range noiseTokens {
			if word == token {
				isNoise = true
				break
			}
		}
		if !isNoise {
			filtered = append(filtered, word)
		}
	}

	// Join and normalize
	result := strings.Join(filtered, " ")

	// CRITICAL FIX: Remove ALL non-alphabetic characters (numbers, punctuation, etc.)
	// This handles cases like "DEPOSIT S ADAMS REF:12345" → "S ADAMS"
	cleaned := make([]rune, 0, len(result))
	for _, r := range result {
		// Keep only letters and spaces
		if (r >= 'A' && r <= 'Z') || r == ' ' {
			cleaned = append(cleaned, r)
		}
	}
	result = string(cleaned)

	// Normalize spaces (collapse multiple spaces)
	result = normalizeName(result)

	// Filter noise tokens AGAIN after removing non-alphabetic characters
	// This catches cases like "REF:12345" → "REF" after cleaning
	wordsAfterClean := strings.Fields(result)
	finalFiltered := make([]string, 0, len(wordsAfterClean))
	for _, word := range wordsAfterClean {
		isNoise := false
		for _, token := range noiseTokens {
			if word == token {
				isNoise = true
				break
			}
		}
		if !isNoise && len(word) > 0 {
			finalFiltered = append(finalFiltered, word)
		}
	}
	result = strings.Join(finalFiltered, " ")

	// If result is empty or too short after cleaning, return empty
	if len(strings.TrimSpace(result)) < 2 {
		return ""
	}

	return strings.TrimSpace(result)
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

// Jaro-Winkler similarity - proper implementation
func jaroWinkler(s1, s2 string) float64 {
	if s1 == s2 {
		return 100.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Normalize to uppercase for comparison
	s1Upper := strings.ToUpper(s1)
	s2Upper := strings.ToUpper(s2)

	// Calculate Jaro similarity
	jaro := jaroSimilarity(s1Upper, s2Upper)

	// Calculate Winkler prefix bonus
	prefixLen := commonPrefixLength(s1Upper, s2Upper, 4)

	// Winkler modification: jaro + (0.1 * prefixLen * (1 - jaro))
	winkler := jaro + (0.1 * float64(prefixLen) * (1.0 - jaro))

	return winkler * 100.0
}

// jaroSimilarity calculates the Jaro similarity between two strings
func jaroSimilarity(s1, s2 string) float64 {
	if len(s1) == 0 && len(s2) == 0 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Matching window: characters are considered matching if they are within
	// max(len(s1), len(s2))/2 - 1 characters of each other
	matchWindow := (int(math.Max(float64(len(s1)), float64(len(s2)))) / 2) - 1
	if matchWindow < 0 {
		matchWindow = 0
	}

	// Track which characters in each string have been matched
	s1Matches := make([]bool, len(s1))
	s2Matches := make([]bool, len(s2))

	matches := 0

	// Find matches in s1
	for i := 0; i < len(s1); i++ {
		start := int(math.Max(0, float64(i-matchWindow)))
		end := int(math.Min(float64(len(s2)), float64(i+matchWindow+1)))

		for j := start; j < end; j++ {
			if s2Matches[j] {
				continue
			}
			if s1[i] == s2[j] {
				s1Matches[i] = true
				s2Matches[j] = true
				matches++
				break
			}
		}
	}

	if matches == 0 {
		return 0.0
	}

	// Count transpositions (characters that match but are in different positions)
	transpositions := 0
	k := 0
	for i := 0; i < len(s1); i++ {
		if !s1Matches[i] {
			continue
		}
		for !s2Matches[k] {
			k++
		}
		if s1[i] != s2[k] {
			transpositions++
		}
		k++
	}

	// Jaro formula: (m/|s1| + m/|s2| + (m-t)/m) / 3
	// where m = matches, t = transpositions/2
	m := float64(matches)
	t := float64(transpositions) / 2.0

	jaro := (m/float64(len(s1)) + m/float64(len(s2)) + (m-t)/m) / 3.0

	return jaro
}

// commonPrefixLength returns the length of the common prefix (up to maxLen)
func commonPrefixLength(s1, s2 string, maxLen int) int {
	prefixLen := 0
	max := int(math.Min(float64(maxLen), math.Min(float64(len(s1)), float64(len(s2)))))

	for i := 0; i < max; i++ {
		if s1[i] == s2[i] {
			prefixLen++
		} else {
			break
		}
	}

	return prefixLen
}
