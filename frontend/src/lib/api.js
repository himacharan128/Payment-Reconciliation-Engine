// API client - single source of truth
import axios from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Accept': 'application/json',
  },
});

// Error handler
const handleError = (error) => {
  if (error.response) {
    const message = error.response.data?.error || error.response.statusText;
    throw { status: error.response.status, message };
  }
  throw { status: 0, message: error.message || 'Network error' };
};

// Upload & Processing
export const uploadCSV = async (file) => {
  const formData = new FormData();
  formData.append('file', file);
  const response = await api.post('/api/reconciliation/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return response.data;
};

export const getBatch = async (batchId) => {
  try {
    const response = await api.get(`/api/reconciliation/${batchId}`);
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const listTransactions = async (batchId, { status, limit = 50, cursor } = {}) => {
  try {
    const params = { limit };
    if (status && status !== 'all') params.status = status;
    if (cursor) params.cursor = cursor;
    const response = await api.get(`/api/reconciliation/${batchId}/transactions`, { params });
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

// Transaction Actions
export const getTransaction = async (id) => {
  try {
    const response = await api.get(`/api/transactions/${id}`);
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const confirmTransaction = async (id, notes = '') => {
  try {
    const response = await api.post(`/api/transactions/${id}/confirm`, { notes });
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const rejectTransaction = async (id) => {
  try {
    const response = await api.post(`/api/transactions/${id}/reject`);
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const manualMatch = async (id, invoiceId, notes = '') => {
  try {
    const response = await api.post(`/api/transactions/${id}/match`, { invoiceId, notes });
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const markExternal = async (id) => {
  try {
    const response = await api.post(`/api/transactions/${id}/external`);
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

export const bulkConfirm = async (batchId, notes = '') => {
  try {
    const response = await api.post('/api/transactions/bulk-confirm', { batchId, notes });
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

// Invoice Search
export const searchInvoices = async ({ q, amount, status, fromDate, toDate, limit = 20 } = {}) => {
  try {
    const params = { limit };
    if (q) params.q = q;
    if (amount) params.amount = amount;
    if (status) params.status = status;
    if (fromDate) params.fromDate = fromDate;
    if (toDate) params.toDate = toDate;
    const response = await api.get('/api/invoices/search', { params });
    return response.data;
  } catch (error) {
    throw handleError(error);
  }
};

// Export Unmatched - fetches all unmatched transactions
export const exportUnmatched = async (batchId) => {
  try {
    const allTransactions = [];
    let cursor = null;
    
    // Fetch all unmatched transactions using pagination
    do {
      const params = { status: 'unmatched', limit: 100 };
      if (cursor) params.cursor = cursor;
      const response = await api.get(`/api/reconciliation/${batchId}/transactions`, { params });
      allTransactions.push(...response.data.items);
      cursor = response.data.nextCursor;
    } while (cursor);
    
    return allTransactions;
  } catch (error) {
    throw handleError(error);
  }
};

