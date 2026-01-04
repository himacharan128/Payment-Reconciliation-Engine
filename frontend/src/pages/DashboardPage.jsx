import { useState, useEffect } from 'react';
import { useParams, Link, useSearchParams, useNavigate } from 'react-router-dom';
import { getBatch, listTransactions, bulkConfirm, confirmTransaction, rejectTransaction, markExternal, exportUnmatched } from '../lib/api';
import { toast } from '../components/Toast';
import LoadingSpinner from '../components/LoadingSpinner';
import StatusBadge from '../components/StatusBadge';
import EmptyState from '../components/EmptyState';
import { formatMoney, formatDate, transactionsToCSV, downloadCSV } from '../lib/utils';
import ManualMatchModal from '../components/ManualMatchModal';

const TABS = [
  { id: 'all', label: 'All' },
  { id: 'auto_matched', label: 'Auto-Matched' },
  { id: 'needs_review', label: 'Needs Review' },
  { id: 'unmatched', label: 'Unmatched' },
  { id: 'confirmed', label: 'Confirmed' },
  { id: 'external', label: 'External' },
];

export default function DashboardPage() {
  const { batchId } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [batch, setBatch] = useState(null);
  const [activeTab, setActiveTab] = useState(() => {
    // Initialize from URL params, default to 'all'
    const urlParams = new URLSearchParams(window.location.search);
    const tabFromUrl = urlParams.get('tab');
    return tabFromUrl && TABS.some(t => t.id === tabFromUrl) ? tabFromUrl : 'all';
  });
  const [transactions, setTransactions] = useState([]);
  const [nextCursor, setNextCursor] = useState(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [selectedTransaction, setSelectedTransaction] = useState(null);
  const [showManualMatch, setShowManualMatch] = useState(false);
  const [sortColumn, setSortColumn] = useState(null);
  const [sortDirection, setSortDirection] = useState('asc'); // 'asc' or 'desc'
  const [exporting, setExporting] = useState(false);

  useEffect(() => {
    loadBatch();
  }, [batchId]);

  // Sync activeTab with URL params when they change (e.g., browser back/forward)
  useEffect(() => {
    const tabFromUrl = searchParams.get('tab');
    const newTab = tabFromUrl && TABS.some(t => t.id === tabFromUrl) ? tabFromUrl : 'all';
    if (newTab !== activeTab) {
      setActiveTab(newTab);
      setTransactions([]);
      setNextCursor(null);
    }
  }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (batch) {
      loadTransactions();
    }
  }, [batchId, activeTab, batch]);

  const loadBatch = async () => {
    try {
      const data = await getBatch(batchId);
      setBatch(data);
    } catch (error) {
      toast.error(error.message || 'Failed to load batch');
    }
  };

  const loadTransactions = async (cursor = null) => {
    try {
      setLoading(!cursor);
      setLoadingMore(!!cursor);
      const data = await listTransactions(batchId, {
        status: activeTab === 'all' ? undefined : activeTab,
        limit: 50,
        cursor,
      });
      if (cursor) {
        setTransactions(prev => [...prev, ...data.items]);
      } else {
        setTransactions(data.items);
      }
      setNextCursor(data.nextCursor);
    } catch (error) {
      toast.error(error.message || 'Failed to load transactions');
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  };

  const handleTabChange = (tab) => {
    setActiveTab(tab);
    setTransactions([]);
    setNextCursor(null);
    setSortColumn(null); // Reset sort when changing tabs
    setSortDirection('asc');
    // Update URL params to preserve tab state
    const newSearchParams = new URLSearchParams(searchParams);
    if (tab === 'all') {
      newSearchParams.delete('tab');
    } else {
      newSearchParams.set('tab', tab);
    }
    setSearchParams(newSearchParams, { replace: true });
  };

  const handleLoadMore = () => {
    if (nextCursor && !loadingMore) {
      loadTransactions(nextCursor);
    }
  };

  const handleBulkConfirm = async () => {
    if (!confirm('Confirm all auto-matched transactions?')) return;
    try {
      await bulkConfirm(batchId);
      toast.success('All auto-matched transactions confirmed');
      await loadBatch();
      if (activeTab === 'auto_matched') {
        loadTransactions();
      }
    } catch (error) {
      toast.error(error.message || 'Bulk confirm failed');
    }
  };

  const handleExportUnmatched = async () => {
    if (batch.counts.unmatched === 0) {
      toast.error('No unmatched transactions to export');
      return;
    }
    
    setExporting(true);
    try {
      toast.info('Exporting unmatched transactions...');
      const unmatchedTransactions = await exportUnmatched(batchId);
      
      if (unmatchedTransactions.length === 0) {
        toast.error('No unmatched transactions found');
        return;
      }
      
      const csvContent = transactionsToCSV(unmatchedTransactions);
      const filename = `unmatched-transactions-${batchId.substring(0, 8)}-${new Date().toISOString().split('T')[0]}.csv`;
      downloadCSV(csvContent, filename);
      
      toast.success(`Exported ${unmatchedTransactions.length} unmatched transactions`);
    } catch (error) {
      toast.error(error.message || 'Export failed');
    } finally {
      setExporting(false);
    }
  };

  const handleAction = async (action, transactionId) => {
    try {
      switch (action) {
        case 'confirm':
          await confirmTransaction(transactionId);
          toast.success('Transaction confirmed');
          break;
        case 'reject':
          await rejectTransaction(transactionId);
          toast.success('Match rejected');
          break;
        case 'external':
          await markExternal(transactionId);
          toast.success('Marked as external');
          break;
      }
      // Optimistic update
      setTransactions(transactions.map(t =>
        t.id === transactionId
          ? { ...t, status: action === 'confirm' ? 'confirmed' : action === 'reject' ? 'unmatched' : 'external' }
          : t
      ));
      // Refresh batch counters and transaction list to ensure accurate counts
      await Promise.all([
        loadBatch(),
        loadTransactions() // Refresh transactions to update "Confirmed" count accurately
      ]);
    } catch (error) {
      toast.error(error.message || 'Action failed');
    }
  };

  const handleManualMatch = (transaction) => {
    setSelectedTransaction(transaction);
    setShowManualMatch(true);
  };

  const handleManualMatchSuccess = () => {
    setShowManualMatch(false);
    setSelectedTransaction(null);
    loadTransactions();
    loadBatch();
  };

  const handleSort = (column) => {
    if (sortColumn === column) {
      // Toggle direction if same column
      setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc');
    } else {
      // New column, default to ascending
      setSortColumn(column);
      setSortDirection('asc');
    }
  };

  const getSortedTransactions = () => {
    if (!sortColumn) return transactions;

    return [...transactions].sort((a, b) => {
      let aVal, bVal;

      switch (sortColumn) {
        case 'date':
          aVal = new Date(a.transactionDate).getTime();
          bVal = new Date(b.transactionDate).getTime();
          break;
        case 'amount':
          aVal = parseFloat(a.amount) || 0;
          bVal = parseFloat(b.amount) || 0;
          break;
        case 'confidence':
          aVal = a.confidenceScore || 0;
          bVal = b.confidenceScore || 0;
          break;
        default:
          return 0;
      }

      if (aVal < bVal) return sortDirection === 'asc' ? -1 : 1;
      if (aVal > bVal) return sortDirection === 'asc' ? 1 : -1;
      return 0;
    });
  };

  const SortArrow = ({ column }) => {
    if (sortColumn !== column) {
      return <span className="text-gray-400 ml-1">↕</span>;
    }
    return (
      <span className="ml-1">
        {sortDirection === 'asc' ? '↑' : '↓'}
      </span>
    );
  };

  if (!batch) {
    return (
      <div className="flex justify-center items-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto p-6">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Reconciliation Dashboard</h1>
        <button
          onClick={() => navigate('/reconciliation/new')}
          className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 transition-colors flex items-center gap-2"
        >
          <span>+</span>
          Upload New Receipt
        </button>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-5 gap-4 mb-6">
        <div className="bg-white p-4 rounded shadow">
          <p className="text-sm text-gray-600">Total</p>
          <p className="text-2xl font-bold">{batch.totalTransactions || batch.processedCount}</p>
        </div>
        <div className="bg-white p-4 rounded shadow">
          <p className="text-sm text-gray-600">Auto-Matched</p>
          <p className="text-2xl font-bold">{batch.counts.autoMatched}</p>
          <p className="text-sm text-gray-500 mt-1">{formatMoney(batch.totals?.autoMatched || 0)}</p>
        </div>
        <div className="bg-white p-4 rounded shadow">
          <p className="text-sm text-gray-600">Needs Review</p>
          <p className="text-2xl font-bold">{batch.counts.needsReview}</p>
          <p className="text-sm text-gray-500 mt-1">{formatMoney(batch.totals?.needsReview || 0)}</p>
        </div>
        <div className="bg-white p-4 rounded shadow">
          <p className="text-sm text-gray-600">Unmatched</p>
          <p className="text-2xl font-bold">{batch.counts.unmatched}</p>
          <p className="text-sm text-gray-500 mt-1">{formatMoney(batch.totals?.unmatched || 0)}</p>
        </div>
        <div className="bg-white p-4 rounded shadow">
          <p className="text-sm text-gray-600">Confirmed</p>
          <p className="text-2xl font-bold">{batch.counts.confirmed || 0}</p>
          <p className="text-sm text-gray-500 mt-1">{formatMoney(batch.totals?.confirmed || 0)}</p>
        </div>
      </div>

      {/* Bulk Actions */}
      <div className="mb-4 flex gap-2">
        {batch.counts.autoMatched > 0 && (
          <button
            onClick={handleBulkConfirm}
            className="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 transition-colors"
          >
            Confirm All Auto-Matched
          </button>
        )}
        {batch.counts.unmatched > 0 && (
          <button
            onClick={handleExportUnmatched}
            disabled={exporting}
            className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {exporting ? 'Exporting...' : 'Export Unmatched'}
          </button>
        )}
      </div>

      {/* Tabs */}
      <div className="border-b mb-4">
        <div className="flex space-x-4">
          {TABS.map(tab => (
            <button
              key={tab.id}
              onClick={() => handleTabChange(tab.id)}
              className={`px-4 py-2 border-b-2 ${
                activeTab === tab.id
                  ? 'border-blue-600 text-blue-600'
                  : 'border-transparent text-gray-600 hover:text-gray-900'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {/* Transactions Table */}
      {loading ? (
        <div className="flex justify-center py-12">
          <LoadingSpinner size="lg" />
        </div>
      ) : transactions.length === 0 ? (
        <EmptyState message="No transactions found" />
      ) : (
        <>
          <div className="bg-white rounded shadow overflow-hidden">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th 
                    className="px-4 py-3 text-left text-xs font-medium text-gray-500 cursor-pointer hover:bg-gray-100 select-none"
                    onClick={() => handleSort('date')}
                  >
                    <div className="flex items-center">
                      Date
                      <SortArrow column="date" />
                    </div>
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500">Description</th>
                  <th 
                    className="px-4 py-3 text-left text-xs font-medium text-gray-500 cursor-pointer hover:bg-gray-100 select-none"
                    onClick={() => handleSort('amount')}
                  >
                    <div className="flex items-center">
                      Amount
                      <SortArrow column="amount" />
                    </div>
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500">Matched To</th>
                  <th 
                    className="px-4 py-3 text-left text-xs font-medium text-gray-500 cursor-pointer hover:bg-gray-100 select-none"
                    onClick={() => handleSort('confidence')}
                  >
                    <div className="flex items-center">
                      Confidence
                      <SortArrow column="confidence" />
                    </div>
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500">Status</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500">Actions</th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {getSortedTransactions().map((txn) => (
                  <tr key={txn.id}>
                    <td className="px-4 py-3 text-sm">{formatDate(txn.transactionDate)}</td>
                    <td className="px-4 py-3 text-sm">{txn.description}</td>
                    <td className="px-4 py-3 text-sm font-medium">{formatMoney(txn.amount)}</td>
                    <td className="px-4 py-3 text-sm">
                      {txn.matchedInvoiceId ? (
                        <Link 
                          to={`/transactions/${txn.id}?batchId=${batchId}&tab=${activeTab}`} 
                          className="text-blue-600 hover:underline"
                        >
                          View
                        </Link>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm">
                      {txn.confidenceScore ? `${txn.confidenceScore.toFixed(1)}%` : '—'}
                    </td>
                    <td className="px-4 py-3 text-sm">
                      <StatusBadge status={txn.status} />
                    </td>
                    <td className="px-4 py-3 text-sm">
                      <div className="flex gap-2">
                        {txn.status === 'auto_matched' && (
                          <>
                            <button
                              onClick={() => handleAction('confirm', txn.id)}
                              className="px-3 py-1 bg-green-600 text-white text-xs rounded hover:bg-green-700 transition-colors"
                            >
                              Confirm
                            </button>
                            <button
                              onClick={() => handleManualMatch(txn)}
                              className="px-3 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 transition-colors"
                            >
                              Override
                            </button>
                          </>
                        )}
                        {txn.status === 'needs_review' && (
                          <>
                            <button
                              onClick={() => handleAction('confirm', txn.id)}
                              className="px-3 py-1 bg-green-600 text-white text-xs rounded hover:bg-green-700 transition-colors"
                            >
                              Confirm
                            </button>
                            <button
                              onClick={() => handleAction('reject', txn.id)}
                              className="px-3 py-1 bg-red-600 text-white text-xs rounded hover:bg-red-700 transition-colors"
                            >
                              Reject
                            </button>
                            <button
                              onClick={() => handleManualMatch(txn)}
                              className="px-3 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 transition-colors"
                            >
                              Reassign
                            </button>
                          </>
                        )}
                        {txn.status === 'unmatched' && (
                          <>
                            <button
                              onClick={() => handleManualMatch(txn)}
                              className="px-3 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 transition-colors"
                            >
                              Find Match
                            </button>
                            <button
                              onClick={() => handleAction('external', txn.id)}
                              className="px-3 py-1 bg-gray-600 text-white text-xs rounded hover:bg-gray-700 transition-colors"
                            >
                              Mark External
                            </button>
                          </>
                        )}
                        {(txn.status === 'confirmed' || txn.status === 'external') && (
                          <Link
                            to={`/transactions/${txn.id}?batchId=${batchId}&tab=${activeTab}`}
                            className="px-3 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 transition-colors inline-block"
                          >
                            View
                          </Link>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Load More */}
          {nextCursor && (
            <div className="mt-4 text-center">
              <button
                onClick={handleLoadMore}
                disabled={loadingMore}
                className="px-4 py-2 bg-gray-200 rounded hover:bg-gray-300 disabled:opacity-50"
              >
                {loadingMore ? 'Loading...' : 'Load More'}
              </button>
            </div>
          )}
        </>
      )}

      {showManualMatch && selectedTransaction && (
        <ManualMatchModal
          transaction={selectedTransaction}
          onClose={() => {
            setShowManualMatch(false);
            setSelectedTransaction(null);
          }}
          onSuccess={handleManualMatchSuccess}
        />
      )}
    </div>
  );
}

