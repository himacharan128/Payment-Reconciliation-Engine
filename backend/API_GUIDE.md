# API Guide

Base URL: `http://localhost:8080`

All endpoints return JSON. Error responses follow format: `{"error": "error message"}`

---

## Upload & Processing

### Upload CSV
**POST** `/api/reconciliation/upload`

Upload a CSV file to start reconciliation.

**Request:**
- Content-Type: `multipart/form-data`
- Field name: `file`
- File must be CSV with columns: `id`, `transaction_date`, `description`, `amount`, `reference_number`

**Response:** `201 Created`
```json
{
  "batchId": "uuid",
  "status": "processing"
}
```

**Errors:**
- `400` - No file, invalid CSV header, missing columns, empty file
- `413` - File too large (>50MB)
- `500` - Server error

---

### Get Batch Status
**GET** `/api/reconciliation/:batchId`

Get batch status and progress for polling.

**Response:** `200 OK`
```json
{
  "batchId": "uuid",
  "status": "processing",
  "processedCount": 450,
  "totalTransactions": null,
  "counts": {
    "autoMatched": 200,
    "needsReview": 150,
    "unmatched": 100
  },
  "startedAt": "2024-01-04T12:00:00Z",
  "completedAt": null,
  "updatedAt": "2024-01-04T12:00:00Z",
  "progressPercent": null
}
```

**When completed:**
- `status`: `"completed"` or `"failed"`
- `totalTransactions`: set to final count
- `completedAt`: timestamp
- `progressPercent`: 100.0

**Errors:**
- `404` - Batch not found

---

### List Transactions
**GET** `/api/reconciliation/:batchId/transactions`

Get paginated list of transactions for a batch.

**Query Parameters:**
- `status` (optional) - Filter by status: `auto_matched`, `needs_review`, `unmatched`, `confirmed`, `external`, `pending`, or `all`
- `limit` (optional) - Page size, default 50, max 200
- `cursor` (optional) - Base64 cursor for next page

**Response:** `200 OK`
```json
{
  "items": [
    {
      "id": "uuid",
      "transactionDate": "2024-12-15",
      "amount": "450.00",
      "description": "SMITH JOHN CHK DEP...",
      "status": "auto_matched",
      "confidenceScore": 97.3,
      "matchedInvoiceId": "invoice-uuid",
      "referenceNumber": "REF123"
    }
  ],
  "nextCursor": "base64-encoded-cursor-or-null"
}
```

**Example:**
```
GET /api/reconciliation/{batchId}/transactions?status=needs_review&limit=50
GET /api/reconciliation/{batchId}/transactions?cursor={nextCursor}&limit=50
```

**Errors:**
- `400` - Invalid batchId, invalid status, invalid cursor

---

## Transaction Actions

### Get Transaction Detail
**GET** `/api/transactions/:id`

Get full transaction details with match explanation.

**Response:** `200 OK`
```json
{
  "id": "uuid",
  "uploadBatchId": "batch-uuid",
  "transactionDate": "2024-12-15",
  "amount": "450.00",
  "description": "SMITH JOHN CHK DEP",
  "referenceNumber": "REF123",
  "status": "auto_matched",
  "confidenceScore": 97.3,
  "matchedInvoiceId": "invoice-uuid",
  "matchDetails": {
    "version": "v1",
    "amount": {
      "transaction": "450.00",
      "invoice": "450.00"
    },
    "name": {
      "extracted": "SMITH JOHN",
      "invoiceName": "John David Smith",
      "similarity": 92.3
    },
    "date": {
      "transactionDate": "2024-12-15",
      "invoiceDueDate": "2024-12-10",
      "deltaDays": -5,
      "adjustment": 5.0
    },
    "ambiguity": {
      "candidateCount": 1,
      "penalty": 0.0
    },
    "finalScore": 97.3,
    "bucket": "auto_matched",
    "topCandidates": [...]
  },
  "createdAt": "2024-01-04T12:00:00Z",
  "updatedAt": "2024-01-04T12:00:00Z",
  "invoice": {
    "id": "uuid",
    "invoiceNumber": "INV-2024-001",
    "customerName": "John David Smith",
    "customerEmail": "john@example.com",
    "amount": "450.00",
    "dueDate": "2024-12-10",
    "status": "sent"
  },
  "canConfirm": true,
  "canReject": true,
  "canManualMatch": false,
  "canMarkExternal": false
}
```

**Errors:**
- `400` - Invalid transaction ID format
- `404` - Transaction not found

---

### Confirm Match
**POST** `/api/transactions/:id/confirm`

Confirm a suggested match (auto_matched or needs_review → confirmed).

**Request Body:** (optional)
```json
{
  "notes": "Verified match"
}
```

**Response:** `200 OK`
```json
{
  "message": "match confirmed"
}
```

**Errors:**
- `400` - Invalid transaction ID, cannot confirm (wrong status), no matched invoice
- `404` - Transaction not found

**Note:** Idempotent - returns 200 if already confirmed.

---

### Reject Match
**POST** `/api/transactions/:id/reject`

Reject a suggested match (auto_matched or needs_review → unmatched).

**Response:** `200 OK`
```json
{
  "message": "match rejected"
}
```

**Errors:**
- `400` - Invalid transaction ID, cannot reject (wrong status)
- `404` - Transaction not found

---

### Manual Match
**POST** `/api/transactions/:id/match`

Manually assign an invoice to a transaction.

**Request Body:**
```json
{
  "invoiceId": "uuid",
  "notes": "Manual assignment"
}
```

**Response:** `200 OK`
```json
{
  "message": "invoice manually matched"
}
```

**Errors:**
- `400` - Invalid transaction/invoice ID, invoice is paid
- `404` - Transaction or invoice not found

**Note:** Sets status to `confirmed` and confidence to 100.0

---

### Mark External
**POST** `/api/transactions/:id/external`

Mark transaction as external (no invoice in system).

**Response:** `200 OK`
```json
{
  "message": "marked as external"
}
```

**Errors:**
- `400` - Invalid transaction ID
- `404` - Transaction not found

**Note:** Idempotent - returns 200 if already external.

---

### Bulk Confirm
**POST** `/api/transactions/bulk-confirm`

Confirm all auto_matched transactions in a batch.

**Request Body:**
```json
{
  "batchId": "uuid",
  "notes": "Bulk confirmation"
}
```

**Response:** `200 OK`
```json
{
  "message": "bulk confirm completed",
  "confirmed": 200,
  "duration": "1.2s"
}
```

**Errors:**
- `400` - Invalid batch ID
- `500` - Server error

**Note:** Only affects `auto_matched` transactions. Idempotent.

---

## Invoice Search

### Search Invoices
**GET** `/api/invoices/search`

Search invoices for manual matching.

**Query Parameters:**
- `q` (required if no amount/status) - Text search (customer name or invoice number), min 2 chars
- `amount` (optional) - Exact amount match
- `status` (optional) - Filter by status: `draft`, `sent`, `paid`, `overdue`
- `limit` (optional) - Results limit, default 20, max 50

**Response:** `200 OK`
```json
{
  "items": [
    {
      "id": "uuid",
      "invoiceNumber": "INV-2024-001",
      "customerName": "John Smith",
      "customerEmail": "john@example.com",
      "amount": "450.00",
      "dueDate": "2024-12-10",
      "status": "sent"
    }
  ]
}
```

**Examples:**
```
GET /api/invoices/search?q=smith&limit=20
GET /api/invoices/search?amount=450.00&status=sent
GET /api/invoices/search?q=INV-2024-001
```

**Errors:**
- `400` - No filters provided, q too short, invalid status/amount

**Note:** Requires at least one filter (q, amount, or status).

---

## Health Check

### Health
**GET** `/health`

Check API health.

**Response:** `200 OK`
```json
{
  "status": "ok"
}
```

---

## Status Values

### Transaction Status
- `pending` - Initial state
- `auto_matched` - System matched with ≥95% confidence
- `needs_review` - System matched with 60-94% confidence
- `unmatched` - No match found (<60% confidence)
- `confirmed` - User confirmed match
- `external` - Marked as external (no invoice)

### Batch Status
- `uploading` - CSV upload in progress
- `processing` - Worker processing transactions
- `completed` - Processing finished successfully
- `failed` - Processing failed

---

## Common Patterns

### Polling Batch Status
```javascript
// Poll every 2 seconds while processing
const pollBatch = async (batchId) => {
  const response = await fetch(`/api/reconciliation/${batchId}`);
  const batch = await response.json();
  
  if (batch.status === 'processing') {
    // Show progress
    console.log(`Processed ${batch.processedCount} of ${batch.totalTransactions || '?'}`);
    setTimeout(() => pollBatch(batchId), 2000);
  } else if (batch.status === 'completed') {
    // Redirect to dashboard
    window.location.href = `/reconciliation/${batchId}`;
  }
};
```

### Cursor Pagination
```javascript
// First page
const response = await fetch(`/api/reconciliation/${batchId}/transactions?limit=50`);
const data = await response.json();

// Next page
if (data.nextCursor) {
  const nextPage = await fetch(
    `/api/reconciliation/${batchId}/transactions?limit=50&cursor=${data.nextCursor}`
  );
}
```

### Error Handling
All endpoints return standard error format:
```json
{
  "error": "error message"
}
```

Check `response.status`:
- `400` - Bad request (validation error)
- `404` - Not found
- `413` - Payload too large
- `500` - Server error

