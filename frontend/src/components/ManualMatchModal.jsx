import { useState, useEffect } from 'react';
import { searchInvoices, manualMatch } from '../lib/api';
import { toast } from './Toast';
import LoadingSpinner from './LoadingSpinner';
import { formatMoney, formatDate } from '../lib/utils';

export default function ManualMatchModal({ transaction, onClose, onSuccess }) {
  const [query, setQuery] = useState('');
  const [amount, setAmount] = useState(transaction.amount || '');
  const [status, setStatus] = useState('sent');
  const [invoices, setInvoices] = useState([]);
  const [loading, setLoading] = useState(false);
  const [matching, setMatching] = useState(false);

  useEffect(() => {
    if (query.length >= 2 || amount) {
      const timeoutId = setTimeout(() => {
        performSearch();
      }, 500);
      return () => clearTimeout(timeoutId);
    } else {
      setInvoices([]);
    }
  }, [query, amount, status]);

  const performSearch = async () => {
    if (!query && !amount) return;
    
    setLoading(true);
    try {
      const params = { limit: 20 };
      if (query) params.q = query;
      if (amount) params.amount = amount;
      if (status) params.status = status;
      const data = await searchInvoices(params);
      setInvoices(data.items);
    } catch (error) {
      toast.error(error.message || 'Search failed');
    } finally {
      setLoading(false);
    }
  };

  const handleSelect = async (invoiceId) => {
    setMatching(true);
    try {
      await manualMatch(transaction.id, invoiceId);
      toast.success('Invoice matched successfully');
      onSuccess();
    } catch (error) {
      toast.error(error.message || 'Match failed');
    } finally {
      setMatching(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 max-w-4xl w-full max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-bold">Find Invoice to Match</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-700">
            ×
          </button>
        </div>

        <div className="mb-4 space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">Search (Name or Invoice #)</label>
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Enter customer name or invoice number"
              className="w-full px-3 py-2 border rounded"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Amount</label>
              <input
                type="number"
                step="0.01"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className="w-full px-3 py-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Status</label>
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                className="w-full px-3 py-2 border rounded"
              >
                <option value="sent">Sent</option>
                <option value="overdue">Overdue</option>
                <option value="draft">Draft</option>
              </select>
            </div>
          </div>
        </div>

        {loading ? (
          <div className="flex justify-center py-8">
            <LoadingSpinner />
          </div>
        ) : invoices.length === 0 ? (
          <p className="text-center text-gray-500 py-8">
            {query.length < 2 && !amount ? 'Enter at least 2 characters or an amount' : 'No invoices found'}
          </p>
        ) : (
          <div className="border rounded">
            <table className="min-w-full">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-2 text-left text-xs font-medium">Invoice #</th>
                  <th className="px-4 py-2 text-left text-xs font-medium">Customer</th>
                  <th className="px-4 py-2 text-left text-xs font-medium">Amount</th>
                  <th className="px-4 py-2 text-left text-xs font-medium">Due Date</th>
                  <th className="px-4 py-2 text-left text-xs font-medium">Status</th>
                  <th className="px-4 py-2 text-left text-xs font-medium">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {invoices.map((invoice) => (
                  <tr key={invoice.id}>
                    <td className="px-4 py-2 text-sm">{invoice.invoiceNumber}</td>
                    <td className="px-4 py-2 text-sm">{invoice.customerName}</td>
                    <td className="px-4 py-2 text-sm">{formatMoney(invoice.amount)}</td>
                    <td className="px-4 py-2 text-sm">{formatDate(invoice.dueDate)}</td>
                    <td className="px-4 py-2 text-sm">{invoice.status}</td>
                    <td className="px-4 py-2 text-sm">
                      <button
                        onClick={() => handleSelect(invoice.id)}
                        disabled={matching}
                        className="px-3 py-1 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
                      >
                        Select
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

