import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ToastContainer } from './components/Toast';
import UploadPage from './pages/UploadPage';
import DashboardPage from './pages/DashboardPage';
import TransactionDetailPage from './pages/TransactionDetailPage';

function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen bg-gray-50">
        <ToastContainer />
        <Routes>
          <Route path="/" element={<Navigate to="/reconciliation/new" replace />} />
          <Route path="/reconciliation/new" element={<UploadPage />} />
          <Route path="/reconciliation/:batchId" element={<DashboardPage />} />
          <Route path="/transactions/:id" element={<TransactionDetailPage />} />
        </Routes>
      </div>
    </BrowserRouter>
  );
}

export default App;
