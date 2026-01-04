#!/usr/bin/env python3
"""
Performance test script for payment reconciliation engine.

Tests:
1. Processing time for 1,000 transactions (bank_transactions.csv)
2. Processing time for 10,000 transactions (bank_transactions_large.csv)
3. Search response time (invoice search and transaction list)
"""

import requests
import time
import json
import sys
from pathlib import Path

BASE_URL = "http://localhost:8080"
POLL_INTERVAL = 1.0  # seconds between status checks
MAX_WAIT_TIME = 300  # maximum wait time in seconds (5 minutes)

def upload_csv(file_path):
    """Upload CSV file and return batch ID."""
    print(f"📤 Uploading {file_path}...")
    
    with open(file_path, 'rb') as f:
        files = {'file': (Path(file_path).name, f, 'text/csv')}
        response = requests.post(f"{BASE_URL}/api/reconciliation/upload", files=files)
    
    if response.status_code != 201:
        print(f"❌ Upload failed: {response.status_code}")
        print(response.text)
        sys.exit(1)
    
    data = response.json()
    batch_id = data['batchId']
    print(f"✅ Upload successful! Batch ID: {batch_id}")
    return batch_id

def get_batch_status(batch_id):
    """Get batch status."""
    response = requests.get(f"{BASE_URL}/api/reconciliation/{batch_id}")
    if response.status_code != 200:
        print(f"❌ Failed to get batch status: {response.status_code}")
        print(response.text)
        return None
    return response.json()

def wait_for_completion(batch_id):
    """Poll batch status until completion and return processing time."""
    print(f"⏳ Waiting for processing to complete...")
    
    start_time = time.time()
    last_status = None
    
    while True:
        elapsed = time.time() - start_time
        if elapsed > MAX_WAIT_TIME:
            print(f"❌ Timeout after {MAX_WAIT_TIME} seconds")
            return None
        
        status = get_batch_status(batch_id)
        if not status:
            return None
        
        current_status = status.get('status')
        processed = status.get('processedCount', 0)
        total = status.get('totalTransactions')
        
        # Show progress if status changed
        if current_status != last_status:
            if total:
                print(f"   Status: {current_status} | Processed: {processed}/{total}")
            else:
                print(f"   Status: {current_status} | Processed: {processed}")
            last_status = current_status
        
        if current_status == 'completed':
            end_time = time.time()
            processing_time = end_time - start_time
            
            counts = status.get('counts', {})
            print(f"✅ Processing completed!")
            print(f"   Total transactions: {total}")
            print(f"   Auto-matched: {counts.get('autoMatched', 0)}")
            print(f"   Needs review: {counts.get('needsReview', 0)}")
            print(f"   Unmatched: {counts.get('unmatched', 0)}")
            print(f"   Processing time: {processing_time:.2f} seconds")
            
            return {
                'processing_time': processing_time,
                'total_transactions': total,
                'counts': counts,
                'batch_id': batch_id
            }
        
        if current_status == 'failed':
            print(f"❌ Processing failed!")
            return None
        
        time.sleep(POLL_INTERVAL)

def test_invoice_search():
    """Test invoice search response time."""
    print("\n🔍 Testing invoice search performance...")
    
    # Test different search queries
    test_cases = [
        {"q": "smith", "description": "Text search (customer name)"},
        {"q": "INV", "description": "Invoice number search"},
        {"amount": "450.00", "description": "Amount search"},
        {"status": "sent", "description": "Status filter"},
        {"q": "john", "amount": "450.00", "description": "Combined search"},
    ]
    
    results = []
    for test_case in test_cases:
        description = test_case.pop('description')
        start_time = time.time()
        
        response = requests.get(f"{BASE_URL}/api/invoices/search", params=test_case)
        
        elapsed = time.time() - start_time
        
        if response.status_code == 200:
            data = response.json()
            result_count = len(data.get('items', []))
            results.append({
                'description': description,
                'response_time': elapsed,
                'result_count': result_count
            })
            print(f"   {description}: {elapsed*1000:.2f}ms ({result_count} results)")
        else:
            print(f"   {description}: ❌ Failed ({response.status_code})")
    
    if results:
        avg_time = sum(r['response_time'] for r in results) / len(results)
        print(f"\n   Average search time: {avg_time*1000:.2f}ms")
        return avg_time
    
    return None

def test_transaction_list(batch_id):
    """Test transaction list response time."""
    print("\n📋 Testing transaction list performance...")
    
    test_cases = [
        {"status": "all", "limit": 50, "description": "All transactions (50)"},
        {"status": "auto_matched", "limit": 50, "description": "Auto-matched (50)"},
        {"status": "needs_review", "limit": 50, "description": "Needs review (50)"},
        {"status": "all", "limit": 200, "description": "All transactions (200)"},
    ]
    
    results = []
    for test_case in test_cases:
        description = test_case.pop('description')
        start_time = time.time()
        
        response = requests.get(
            f"{BASE_URL}/api/reconciliation/{batch_id}/transactions",
            params=test_case
        )
        
        elapsed = time.time() - start_time
        
        if response.status_code == 200:
            data = response.json()
            result_count = len(data.get('items', []))
            results.append({
                'description': description,
                'response_time': elapsed,
                'result_count': result_count
            })
            print(f"   {description}: {elapsed*1000:.2f}ms ({result_count} results)")
        else:
            print(f"   {description}: ❌ Failed ({response.status_code})")
    
    if results:
        avg_time = sum(r['response_time'] for r in results) / len(results)
        print(f"\n   Average list time: {avg_time*1000:.2f}ms")
        return avg_time
    
    return None

def main():
    print("=" * 60)
    print("Payment Reconciliation Engine - Performance Test")
    print("=" * 60)
    
    # Check if backend is running
    try:
        response = requests.get(f"{BASE_URL}/health", timeout=5)
        if response.status_code != 200:
            print(f"❌ Backend health check failed: {response.status_code}")
            sys.exit(1)
    except requests.exceptions.RequestException as e:
        print(f"❌ Cannot connect to backend at {BASE_URL}")
        print(f"   Error: {e}")
        sys.exit(1)
    
    print("✅ Backend is running\n")
    
    results = {}
    
    # Test 1: 1,000 transactions
    print("\n" + "=" * 60)
    print("TEST 1: Processing 1,000 transactions")
    print("=" * 60)
    
    csv_1k = Path("bank_transactions.csv")
    if not csv_1k.exists():
        print(f"❌ File not found: {csv_1k}")
        sys.exit(1)
    
    batch_id_1k = upload_csv(csv_1k)
    result_1k = wait_for_completion(batch_id_1k)
    
    if result_1k:
        results['1000_transactions'] = result_1k
        
        # Test search with 1k batch
        search_time_1k = test_invoice_search()
        list_time_1k = test_transaction_list(batch_id_1k)
        
        if search_time_1k:
            results['1000_transactions']['avg_search_time'] = search_time_1k
        if list_time_1k:
            results['1000_transactions']['avg_list_time'] = list_time_1k
    
    # Test 2: 10,000 transactions
    print("\n" + "=" * 60)
    print("TEST 2: Processing 10,000 transactions")
    print("=" * 60)
    
    csv_10k = Path("bank_transactions_large.csv")
    if not csv_10k.exists():
        print(f"❌ File not found: {csv_10k}")
        print("   Skipping 10k test...")
    else:
        batch_id_10k = upload_csv(csv_10k)
        result_10k = wait_for_completion(batch_id_10k)
        
        if result_10k:
            results['10000_transactions'] = result_10k
            
            # Test search with 10k batch
            search_time_10k = test_invoice_search()
            list_time_10k = test_transaction_list(batch_id_10k)
            
            if search_time_10k:
                results['10000_transactions']['avg_search_time'] = search_time_10k
            if list_time_10k:
                results['10000_transactions']['avg_list_time'] = list_time_10k
    
    # Summary
    print("\n" + "=" * 60)
    print("PERFORMANCE SUMMARY")
    print("=" * 60)
    
    if '1000_transactions' in results:
        r = results['1000_transactions']
        print(f"\n📊 1,000 Transactions:")
        print(f"   Processing time: {r['processing_time']:.2f} seconds")
        if 'avg_search_time' in r:
            print(f"   Average search time: {r['avg_search_time']*1000:.2f}ms")
        if 'avg_list_time' in r:
            print(f"   Average list time: {r['avg_list_time']*1000:.2f}ms")
    
    if '10000_transactions' in results:
        r = results['10000_transactions']
        print(f"\n📊 10,000 Transactions:")
        print(f"   Processing time: {r['processing_time']:.2f} seconds")
        if 'avg_search_time' in r:
            print(f"   Average search time: {r['avg_search_time']*1000:.2f}ms")
        if 'avg_list_time' in r:
            print(f"   Average list time: {r['avg_list_time']*1000:.2f}ms")
    
    # Save results to JSON
    results_file = Path("performance_results.json")
    with open(results_file, 'w') as f:
        json.dump(results, f, indent=2)
    print(f"\n💾 Results saved to: {results_file}")
    
    print("\n" + "=" * 60)

if __name__ == "__main__":
    main()

