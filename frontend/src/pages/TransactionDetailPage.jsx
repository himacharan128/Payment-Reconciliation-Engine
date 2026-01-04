import { useState, useEffect } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { getTransaction, confirmTransaction, rejectTransaction, markExternal } from '../lib/api';
import { toast } from '../components/Toast';
import LoadingSpinner from '../components/LoadingSpinner';
import StatusBadge from '../components/StatusBadge';
import { formatMoney, formatDate } from '../lib/utils';
import ManualMatchModal from '../components/ManualMatchModal';

export default function TransactionDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [transaction, setTransaction] = useState(null);
  const [loading, setLoading] = useState(true);
  const [showManualMatch, setShowManualMatch] = useState(false);

  useEffect(() => {
    loadTransaction();
  }, [id]);

  const loadTransaction = async () => {
    try {
      setLoading(true);
      const data = await getTransaction(id);
      setTransaction(data);
      
      // Store batchId in localStorage for navigation fallback
      if (data.uploadBatchId) {
        localStorage.setItem('lastBatchId', data.uploadBatchId);
      }
    } catch (error) {
      toast.error(error.message || 'Failed to load transaction');
      if (error.status === 404) {
        // Try to navigate back to batch if we have it
        const batchId = new URLSearchParams(window.location.search).get('batchId') || localStorage.getItem('lastBatchId');
        if (batchId) {
          navigate(`/reconciliation/${batchId}`);
        } else {
          navigate('/reconciliation/new');
        }
      }
    } finally {
      setLoading(false);
    }
  };

  const handleAction = async (action) => {
    try {
      switch (action) {
        case 'confirm':
          await confirmTransaction(id);
          toast.success('Transaction confirmed');
          break;
        case 'reject':
          await rejectTransaction(id);
          toast.success('Match rejected');
          break;
        case 'external':
          await markExternal(id);
          toast.success('Marked as external');
          break;
      }
      await loadTransaction();
    } catch (error) {
      toast.error(error.message || 'Action failed');
    }
  };

  if (loading) {
    return (
      <div className="flex justify-center items-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!transaction) {
    return <div className="p-6">Transaction not found</div>;
  }

  const matchDetails = transaction.matchDetails || {};

  // Get batchId and tab from URL or transaction
  const getBatchIdAndTab = () => {
    // Try to get from URL search params first (if we came from dashboard)
    const urlParams = new URLSearchParams(window.location.search);
    const batchIdFromUrl = urlParams.get('batchId');
    const tabFromUrl = urlParams.get('tab') || 'all';
    
    if (batchIdFromUrl) {
      return { batchId: batchIdFromUrl, tab: tabFromUrl };
    }
    
    // Fallback to transaction's uploadBatchId
    if (transaction?.uploadBatchId) {
      return { batchId: transaction.uploadBatchId, tab: 'all' };
    }
    
    // Last resort: try to get from localStorage (if we stored it)
    const lastBatchId = localStorage.getItem('lastBatchId');
    if (lastBatchId) {
      return { batchId: lastBatchId, tab: 'all' };
    }
    
    return { batchId: '/reconciliation/new', tab: 'all' };
  };

  const { batchId, tab } = getBatchIdAndTab();

  return (
    <div className="max-w-6xl mx-auto p-6">
      <div className="mb-4">
        <Link 
          to={typeof batchId === 'string' && batchId.startsWith('/') 
            ? batchId 
            : `/reconciliation/${batchId}${tab !== 'all' ? `?tab=${tab}` : ''}`} 
          className="text-blue-600 hover:underline"
        >
          ← Back to Dashboard
        </Link>
      </div>

      <h1 className="text-2xl font-bold mb-6">Transaction Details</h1>

      <div className="grid grid-cols-2 gap-6">
        {/* Transaction Card */}
        <div className="bg-white p-6 rounded shadow">
          <h2 className="text-lg font-semibold mb-4">Bank Transaction</h2>
          <div className="space-y-2">
            <div>
              <span className="text-sm text-gray-600">Date:</span>
              <span className="ml-2">{formatDate(transaction.transactionDate)}</span>
            </div>
            <div>
              <span className="text-sm text-gray-600">Amount:</span>
              <span className="ml-2 font-semibold">{formatMoney(transaction.amount)}</span>
            </div>
            <div>
              <span className="text-sm text-gray-600">Description:</span>
              <p className="mt-1">{transaction.description}</p>
            </div>
            {transaction.referenceNumber && (
              <div>
                <span className="text-sm text-gray-600">Reference:</span>
                <span className="ml-2">{transaction.referenceNumber}</span>
              </div>
            )}
            <div>
              <span className="text-sm text-gray-600">Status:</span>
              <span className="ml-2">
                <StatusBadge status={transaction.status} />
              </span>
            </div>
            {transaction.confidenceScore && (
              <div>
                <span className="text-sm text-gray-600">Confidence:</span>
                <span className="ml-2">{transaction.confidenceScore.toFixed(1)}%</span>
              </div>
            )}
          </div>
        </div>

        {/* Invoice Card */}
        {transaction.invoice ? (
          <div className="bg-white p-6 rounded shadow">
            <h2 className="text-lg font-semibold mb-4">Matched Invoice</h2>
            <div className="space-y-2">
              <div>
                <span className="text-sm text-gray-600">Invoice #:</span>
                <span className="ml-2 font-medium">{transaction.invoice.invoiceNumber}</span>
              </div>
              <div>
                <span className="text-sm text-gray-600">Customer:</span>
                <span className="ml-2">{transaction.invoice.customerName}</span>
              </div>
              {transaction.invoice.customerEmail && (
                <div>
                  <span className="text-sm text-gray-600">Email:</span>
                  <span className="ml-2">{transaction.invoice.customerEmail}</span>
                </div>
              )}
              <div>
                <span className="text-sm text-gray-600">Amount:</span>
                <span className="ml-2 font-semibold">{formatMoney(transaction.invoice.amount)}</span>
              </div>
              <div>
                <span className="text-sm text-gray-600">Due Date:</span>
                <span className="ml-2">{formatDate(transaction.invoice.dueDate)}</span>
              </div>
              <div>
                <span className="text-sm text-gray-600">Status:</span>
                <span className="ml-2">{transaction.invoice.status}</span>
              </div>
            </div>
          </div>
        ) : (
          <div className="bg-white p-6 rounded shadow">
            <h2 className="text-lg font-semibold mb-4">No Invoice Matched</h2>
            <p className="text-gray-500">This transaction has not been matched to an invoice.</p>
          </div>
        )}
      </div>

      {/* Match Explanation */}
      {matchDetails.version && (
        <div className="bg-white p-6 rounded shadow mt-6">
          <h2 className="text-lg font-semibold mb-4">Match Explanation</h2>
          <div className="space-y-3">
            {matchDetails.name && (
              <div>
                <span className="text-sm font-medium">Name Similarity:</span>
                <span className="ml-2">
                  {matchDetails.name.extracted} vs {matchDetails.name.invoiceName} ={' '}
                  {matchDetails.name.similarity?.toFixed(1)}%
                </span>
              </div>
            )}
            {matchDetails.date && (
              <div>
                <span className="text-sm font-medium">Date:</span>
                <span className="ml-2">
                  {matchDetails.date.deltaDays} days from due date
                  {matchDetails.date.adjustment && ` (${matchDetails.date.adjustment > 0 ? '+' : ''}${matchDetails.date.adjustment} points)`}
                </span>
              </div>
            )}
            {matchDetails.ambiguity && matchDetails.ambiguity.candidateCount > 1 && (
              <div>
                <span className="text-sm font-medium">Ambiguity:</span>
                <span className="ml-2">
                  {matchDetails.ambiguity.candidateCount} candidates found
                  {matchDetails.ambiguity.penalty && ` (${matchDetails.ambiguity.penalty} point penalty)`}
                </span>
              </div>
            )}
            <div>
              <span className="text-sm font-medium">Final Score:</span>
              <span className="ml-2 font-semibold">{matchDetails.finalScore?.toFixed(1)}%</span>
            </div>
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="mt-6 flex gap-2">
        {transaction.canConfirm && (
          <button
            onClick={() => handleAction('confirm')}
            className="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700"
          >
            Confirm Match
          </button>
        )}
        {transaction.canReject && (
          <button
            onClick={() => handleAction('reject')}
            className="px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700"
          >
            Reject Match
          </button>
        )}
        {transaction.canManualMatch && (
          <button
            onClick={() => setShowManualMatch(true)}
            className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700"
          >
            Manual Match
          </button>
        )}
        {transaction.canMarkExternal && (
          <button
            onClick={() => handleAction('external')}
            className="px-4 py-2 bg-gray-600 text-white rounded hover:bg-gray-700"
          >
            Mark External
          </button>
        )}
      </div>

      {showManualMatch && (
        <ManualMatchModal
          transaction={transaction}
          onClose={() => setShowManualMatch(false)}
          onSuccess={() => {
            setShowManualMatch(false);
            loadTransaction();
          }}
        />
      )}
    </div>
  );
}

