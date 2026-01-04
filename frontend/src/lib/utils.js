// Utility functions

// Format money
export const formatMoney = (amount) => {
  const num = parseFloat(amount);
  if (isNaN(num)) return '$0.00';
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(num);
};

// Format date
export const formatDate = (dateString) => {
  if (!dateString) return '';
  const date = new Date(dateString);
  return date.toLocaleDateString('en-US', { 
    year: 'numeric', 
    month: 'short', 
    day: 'numeric' 
  });
};

// Truncate text
export const truncate = (text, maxLength = 50) => {
  if (!text) return '';
  if (text.length <= maxLength) return text;
  return text.substring(0, maxLength) + '...';
};

// Status label mapping
export const getStatusLabel = (status) => {
  const labels = {
    pending: 'Pending',
    auto_matched: 'Auto-Matched',
    needs_review: 'Needs Review',
    unmatched: 'Unmatched',
    confirmed: 'Confirmed',
    external: 'External',
  };
  return labels[status] || status;
};

// Status badge color
export const getStatusColor = (status) => {
  const colors = {
    pending: 'gray',
    auto_matched: 'green',
    needs_review: 'yellow',
    unmatched: 'red',
    confirmed: 'blue',
    external: 'gray',
  };
  return colors[status] || 'gray';
};

// Convert transactions array to CSV format
export const transactionsToCSV = (transactions) => {
  if (transactions.length === 0) return '';
  
  // CSV header
  const headers = ['ID', 'Transaction Date', 'Description', 'Amount', 'Reference Number', 'Status'];
  const rows = [headers.join(',')];
  
  // CSV rows
  transactions.forEach(txn => {
    const row = [
      txn.id || '',
      txn.transactionDate || '',
      `"${(txn.description || '').replace(/"/g, '""')}"`, // Escape quotes in description
      txn.amount || '',
      txn.referenceNumber || '',
      txn.status || '',
    ];
    rows.push(row.join(','));
  });
  
  return rows.join('\n');
};

// Download CSV file
export const downloadCSV = (csvContent, filename) => {
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
  const link = document.createElement('a');
  const url = URL.createObjectURL(blob);
  link.setAttribute('href', url);
  link.setAttribute('download', filename);
  link.style.visibility = 'hidden';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
};

