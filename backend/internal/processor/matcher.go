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
	finalScoreBP     int // finalScore * 100 as integer for deterministic comparison
}

// Global transaction counter for debug logging
var debugTxnCounter int

// MatchTransaction matches a bank transaction against invoice candidates
func MatchTransaction(
	description string,
	amount string,
	transactionDate time.Time,
	candidates []*InvoiceCandidate,
) MatchResult {
	debugTxnCounter++
	txnNum := debugTxnCounter
	
	if len(candidates) == 0 {
		debugLog("TXN#%d: desc=%q amount=%s -> NO_CANDIDATES -> unmatched", 
			txnNum, description, amount)
		return MatchResult{
			Confidence: 0,
			Status:     "unmatched",
			MatchDetails: map[string]interface{}{
				"version": "v1",
				"reason":  "no_invoice_with_matching_amount",
			},
		}
	}

	// Log candidates for this transaction
	debugLog("TXN#%d: desc=%q amount=%s date=%s candidates=%d", 
		txnNum, description, amount, transactionDate.Format("2006-01-02"), len(candidates))
	for i, c := range candidates {
		debugLog("  candidate[%d]: ID=%s DueDate=%s Name=%s", 
			i, c.ID, c.DueDate.Format("2006-01-02"), c.CustomerName)
	}

	// Candidates are pre-sorted in the cache by due_date then ID for deterministic ordering

	// Extract and normalize name from description
	extractedName := extractNameFromDescription(description)
	
	// If name extraction is too weak, cap confidence
	nameTooWeak := len(extractedName) < 3 || strings.TrimSpace(extractedName) == ""
	
	// Score each candidate
	scored := make([]scoredCandidate, 0, len(candidates))
	
	// Pre-process extracted name for better matching
	extractedParts := strings.Fields(extractedName)
	extractedInitials := ""
	if len(extractedParts) > 0 {
		// Build initials (e.g., "S A" -> "SA", "SARAH ADAMS" -> "SA")
		for _, part := range extractedParts {
			if len(part) > 0 {
				extractedInitials += string(part[0])
			}
		}
	}
	
	for _, cand := range candidates {
		// Name similarity (primary factor, 0-100)
		nameScore := jaroWinkler(extractedName, cand.NormalizedName)
		
		// Boost score for initial matches (e.g., "S ADAMS" vs "SARAH ADAMS")
		if len(extractedInitials) >= 2 {
			candParts := strings.Fields(cand.NormalizedName)
			candInitials := ""
			for _, part := range candParts {
				if len(part) > 0 {
					candInitials += string(part[0])
				}
			}
			
			// If initials match, give a significant boost
			if extractedInitials == candInitials {
				nameScore = math.Max(nameScore, 85.0) // Boost to 85+ for initial matches
			} else if len(extractedInitials) > 0 && len(candInitials) > 0 {
				// Partial initial match (e.g., "SA" matches "SAR")
				if strings.HasPrefix(candInitials, extractedInitials) || strings.HasPrefix(extractedInitials, candInitials) {
					nameScore = math.Max(nameScore, 75.0) // Partial boost
				}
			}
		}
		
		// Also check for last name only match (common in bank descriptions)
		extractedTokens := strings.Fields(extractedName)
		candTokens := strings.Fields(cand.NormalizedName)
		if len(extractedTokens) > 0 && len(candTokens) > 0 {
			// Check if last token matches
			extractedLast := extractedTokens[len(extractedTokens)-1]
			candLast := candTokens[len(candTokens)-1]
			if len(extractedLast) >= 3 && extractedLast == candLast {
				// Last name matches exactly
				nameScore = math.Max(nameScore, 80.0)
			}
		}
		
		// If name extraction is weak, cap name score contribution
		if nameTooWeak {
			nameScore = math.Min(nameScore, 50.0) // Cap at 50 if name extraction failed
		}
		
		// Date proximity adjustment (-10 to +5 points)
		dateDelta := int(transactionDate.Sub(cand.DueDate).Hours() / 24)
		dateAdjustment := calculateDateAdjustment(dateDelta)
		
		// Ambiguity penalty (if multiple candidates)
		// Reduced penalty to allow more auto-matches when name similarity is high
		// Only apply significant penalty for 4+ candidates
		ambiguityPenalty := 0.0
		if len(candidates) > 3 {
			ambiguityPenalty = float64(len(candidates)-3) * 1.0 // -1 point per extra candidate beyond 3
		}
		
		// Final score: nameScore + dateAdjustment - ambiguityPenalty
		finalScore := nameScore + dateAdjustment - ambiguityPenalty
		finalScore = math.Max(0, math.Min(100, finalScore)) // Clamp 0-100
		finalScore = math.Round(finalScore*100) / 100 // Round to 2 decimals
		
		// Convert to basis points (integer) for deterministic comparison
		finalScoreBP := int(math.Round(finalScore * 100))
		
		scored = append(scored, scoredCandidate{
			candidate:        cand,
			nameScore:        nameScore,
			dateDelta:        dateDelta,
			dateAdjustment:   dateAdjustment,
			ambiguityPenalty: ambiguityPenalty,
			finalScore:       finalScore,
			finalScoreBP:     finalScoreBP,
		})
	}
	
	// Sort by score descending with STRICT TOTAL ORDERING
	// Using integer basis points (finalScoreBP) eliminates float comparison issues
	// Every comparison path must return a definitive answer - no "equal" cases left unresolved
	sort.SliceStable(scored, func(i, j int) bool {
		// Primary: higher score wins (using integer basis points for exact comparison)
		if scored[i].finalScoreBP != scored[j].finalScoreBP {
			return scored[i].finalScoreBP > scored[j].finalScoreBP
		}
		
		// Tie-breaker 1: smaller absolute date delta
		absDeltaI := int(math.Abs(float64(scored[i].dateDelta)))
		absDeltaJ := int(math.Abs(float64(scored[j].dateDelta)))
		if absDeltaI != absDeltaJ {
			return absDeltaI < absDeltaJ
		}
		
		// Tie-breaker 2: earlier due date
		if !scored[i].candidate.DueDate.Equal(scored[j].candidate.DueDate) {
			return scored[i].candidate.DueDate.Before(scored[j].candidate.DueDate)
		}
		
		// Tie-breaker 3: invoice ID for FINAL deterministic ordering
		// This ensures we NEVER have two elements that compare "equal"
		return scored[i].candidate.ID < scored[j].candidate.ID
	})
	
	// Log scored candidates after sorting
	debugLog("  SCORED (after sort):")
	for i, s := range scored {
		debugLog("    [%d] ID=%s scoreBP=%d score=%.2f nameScore=%.2f delta=%d", 
			i, s.candidate.ID, s.finalScoreBP, s.finalScore, s.nameScore, s.dateDelta)
	}
	
	best := scored[0]
	
	// Determine status based on final confidence score
	// Thresholds as per BRD:
	// - Auto-matched: ≥95% (high confidence)
	// - Needs review: 60-94% (medium confidence, needs human confirmation)
	// - Unmatched: <60% (low confidence)
	status := "unmatched"
	if best.finalScore >= 95.0 {
		status = "auto_matched"
	} else if best.finalScore >= 60.0 {
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
	
	debugLog("  RESULT: status=%s bestID=%s score=%.2f", status, best.candidate.ID, best.finalScore)
	debugLog("")
	
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
	// Noise tokens commonly found in bank descriptions
	noiseTokens := map[string]bool{
		"CHK": true, "DEP": true, "PMT": true, "PAYMENT": true, "ONLINE": true,
		"TRANSFER": true, "ACH": true, "DEPOSIT": true, "WIRE": true, "CHECK": true,
		"REF": true, "REFERENCE": true, "MISC": true, "DEBIT": true, "CREDIT": true,
		"TXN": true, "TRANSACTION": true, "FEE": true, "CHARGE": true, "FROM": true,
		"TO": true, "VIA": true, "ATM": true, "POS": true, "MOBILE": true, "WEB": true,
		"EXTERNAL": true, "INTERNAL": true, "INCOMING": true, "OUTGOING": true,
		"COUNTER": true, "VENDOR": true, "REBATE": true, "UNKNOWN": true, "BANK": true,
		"CASH": true, "PURCHASE": true,
	}
	
	desc = strings.ToUpper(desc)
	
	// Remove all non-alphabetic characters except spaces
	var cleaned strings.Builder
	for _, ch := range desc {
		if (ch >= 'A' && ch <= 'Z') || ch == ' ' {
			cleaned.WriteRune(ch)
		}
	}
	desc = cleaned.String()
	
	// Split into words and filter out noise tokens
	words := strings.Fields(desc)
	var filteredWords []string
	
	for _, word := range words {
		if noiseTokens[word] {
			continue // Skip noise tokens
		}
		// Keep single letters (likely initials like "S" in "S ADAMS")
		// Keep all words 2+ chars that aren't noise
		if len(word) >= 1 {
			filteredWords = append(filteredWords, word)
		}
	}
	
	// Join and normalize
	return normalizeName(strings.Join(filteredWords, " "))
}

func calculateDateAdjustment(daysDelta int) float64 {
	// Transaction before due date: +5 points (early payment is strong signal)
	if daysDelta < 0 {
		return 5.0
	}
	// Transaction on or near due date (0-7 days): +3 points
	if daysDelta <= 7 {
		return 3.0
	}
	// Transaction 8-14 days after: +1 point (still reasonable)
	if daysDelta <= 14 {
		return 1.0
	}
	// Transaction 15-30 days after: 0 points
	if daysDelta <= 30 {
		return 0.0
	}
	// Transaction >30 days after: -5 points
	return -5.0
}

// jaroWinkler calculates enhanced name similarity that handles:
// 1. Standard Jaro-Winkler character similarity
// 2. Token reordering (SMITH JOHN vs JOHN SMITH)
// 3. Token overlap for partial matches
func jaroWinkler(s1, s2 string) float64 {
	s1 = strings.ToUpper(s1)
	s2 = strings.ToUpper(s2)
	
	if s1 == s2 {
		return 100.0
	}
	
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}
	
	// Calculate multiple similarity measures and take the best
	scores := []float64{
		jaroWinklerRaw(s1, s2),                    // Standard character-based
		tokenSortedJaroWinkler(s1, s2),            // Compare with sorted tokens
		tokenOverlapScore(s1, s2),                 // Token intersection score
	}
	
	// Return the maximum score
	maxScore := scores[0]
	for _, score := range scores[1:] {
		if score > maxScore {
			maxScore = score
		}
	}
	
	return maxScore
}

// tokenSortedJaroWinkler sorts tokens alphabetically before comparing
// This handles "SMITH JOHN" vs "JOHN SMITH" → both become "JOHN SMITH"
func tokenSortedJaroWinkler(s1, s2 string) float64 {
	tokens1 := strings.Fields(s1)
	tokens2 := strings.Fields(s2)
	
	sort.Strings(tokens1)
	sort.Strings(tokens2)
	
	sorted1 := strings.Join(tokens1, " ")
	sorted2 := strings.Join(tokens2, " ")
	
	return jaroWinklerRaw(sorted1, sorted2)
}

// tokenOverlapScore calculates similarity based on token intersection
// Handles partial matches like "S ADAMS" vs "SARAH ADAMS"
func tokenOverlapScore(s1, s2 string) float64 {
	tokens1 := strings.Fields(s1)
	tokens2 := strings.Fields(s2)
	
	if len(tokens1) == 0 || len(tokens2) == 0 {
		return 0.0
	}
	
	// Count exact token matches
	exactMatches := 0
	partialMatches := 0.0
	
	matched2 := make([]bool, len(tokens2))
	
	for _, t1 := range tokens1 {
		bestMatch := 0.0
		bestIdx := -1
		
		for j, t2 := range tokens2 {
			if matched2[j] {
				continue
			}
			
			// Exact match
			if t1 == t2 {
				if bestMatch < 100.0 {
					bestMatch = 100.0
					bestIdx = j
				}
			} else if len(t1) == 1 && len(t2) > 1 && t2[0] == t1[0] {
				// Initial match: "S" matches "SARAH"
				score := 90.0
				if score > bestMatch {
					bestMatch = score
					bestIdx = j
				}
			} else if len(t2) == 1 && len(t1) > 1 && t1[0] == t2[0] {
				// Reverse initial match: "SARAH" matches "S"
				score := 90.0
				if score > bestMatch {
					bestMatch = score
					bestIdx = j
				}
			} else {
				// Partial string match
				score := jaroWinklerRaw(t1, t2)
				if score > bestMatch && score >= 80.0 {
					bestMatch = score
					bestIdx = j
				}
			}
		}
		
		if bestIdx >= 0 {
			matched2[bestIdx] = true
			if bestMatch >= 100.0 {
				exactMatches++
			}
			partialMatches += bestMatch
		}
	}
	
	// Calculate score based on coverage
	maxTokens := float64(max(len(tokens1), len(tokens2)))
	minTokens := float64(min(len(tokens1), len(tokens2)))
	
	// Weighted average: consider both coverage and quality of matches
	coverageScore := partialMatches / maxTokens
	
	// Bonus for matching all tokens in shorter string
	if exactMatches == int(minTokens) {
		coverageScore = math.Max(coverageScore, 90.0)
	}
	
	return coverageScore
}

// jaroWinklerRaw is the standard Jaro-Winkler algorithm
func jaroWinklerRaw(s1, s2 string) float64 {
	if s1 == s2 {
		return 100.0
	}
	
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}
	
	r1 := []rune(s1)
	r2 := []rune(s2)
	len1 := len(r1)
	len2 := len(r2)
	
	// Match window
	matchWindow := max(len1, len2)/2 - 1
	if matchWindow < 1 {
		matchWindow = 1
	}
	
	s1Matches := make([]bool, len1)
	s2Matches := make([]bool, len2)
	
	matches := 0
	transpositions := 0
	
	// Find matches
	for i := 0; i < len1; i++ {
		start := max(0, i-matchWindow)
		end := min(len2, i+matchWindow+1)
		
		for j := start; j < end; j++ {
			if s2Matches[j] || r1[i] != r2[j] {
				continue
			}
			s1Matches[i] = true
			s2Matches[j] = true
			matches++
			break
		}
	}
	
	if matches == 0 {
		return 0.0
	}
	
	// Count transpositions
	k := 0
	for i := 0; i < len1; i++ {
		if !s1Matches[i] {
			continue
		}
		for !s2Matches[k] {
			k++
		}
		if r1[i] != r2[k] {
			transpositions++
		}
		k++
	}
	
	// Jaro similarity
	jaro := (float64(matches)/float64(len1) +
		float64(matches)/float64(len2) +
		float64(matches-transpositions/2)/float64(matches)) / 3.0
	
	// Winkler prefix bonus
	prefixLen := 0
	maxPrefix := min(4, min(len1, len2))
	for i := 0; i < maxPrefix; i++ {
		if r1[i] == r2[i] {
			prefixLen++
		} else {
			break
		}
	}
	
	winkler := jaro + (0.1 * float64(prefixLen) * (1.0 - jaro))
	
	return winkler * 100.0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
