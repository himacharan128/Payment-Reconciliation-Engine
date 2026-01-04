import { useState, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { uploadCSV, getBatch } from '../lib/api';
import { toast } from '../components/Toast';
import LoadingSpinner from '../components/LoadingSpinner';

export default function UploadPage() {
  const [file, setFile] = useState(null);
  const [uploading, setUploading] = useState(false);
  const [processing, setProcessing] = useState(false);
  const [batchId, setBatchId] = useState(null);
  const [progress, setProgress] = useState(null);
  const [estimatedTotal, setEstimatedTotal] = useState(null);
  const fileInputRef = useRef(null);
  const navigate = useNavigate();

  const countCSVRows = async (file) => {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        try {
          const text = e.target.result;
          const lines = text.split('\n').filter(line => line.trim() !== '');
          // Subtract 1 for header row
          const rowCount = Math.max(0, lines.length - 1);
          resolve(rowCount);
        } catch (error) {
          reject(error);
        }
      };
      reader.onerror = () => reject(new Error('Failed to read file'));
      reader.readAsText(file);
    });
  };

  const handleFileSelect = async (e) => {
    const selectedFile = e.target.files?.[0];
    if (selectedFile) {
      if (!selectedFile.name.toLowerCase().endsWith('.csv')) {
        toast.error('Please select a CSV file');
        return;
      }
      if (selectedFile.size > 50 * 1024 * 1024) {
        toast.error('File size must be less than 50MB');
        return;
      }
      setFile(selectedFile);
      
      // Count rows in CSV file
      try {
        const rowCount = await countCSVRows(selectedFile);
        setEstimatedTotal(rowCount);
      } catch (error) {
        console.error('Failed to count CSV rows:', error);
        setEstimatedTotal(null);
      }
    }
  };

  const handleDrop = async (e) => {
    e.preventDefault();
    const droppedFile = e.dataTransfer.files[0];
    if (droppedFile) {
      if (!droppedFile.name.toLowerCase().endsWith('.csv')) {
        toast.error('Please drop a CSV file');
        return;
      }
      setFile(droppedFile);
      
      // Count rows in CSV file
      try {
        const rowCount = await countCSVRows(droppedFile);
        setEstimatedTotal(rowCount);
      } catch (error) {
        console.error('Failed to count CSV rows:', error);
        setEstimatedTotal(null);
      }
    }
  };

  const handleDragOver = (e) => {
    e.preventDefault();
  };

  const handleUpload = async () => {
    if (!file) {
      toast.error('Please select a file');
      return;
    }

    setUploading(true);
    try {
      const result = await uploadCSV(file);
      setBatchId(result.batchId);
      toast.success('File uploaded successfully! Processing...');
      // Set processing before clearing uploading to avoid UI flash
      setProcessing(true);
      setUploading(false);
      pollBatchStatus(result.batchId);
    } catch (error) {
      setUploading(false);
      toast.error(error.message || 'Upload failed');
    }
  };

  const handleReset = () => {
    setFile(null);
    setBatchId(null);
    setProcessing(false);
    setProgress(null);
    setEstimatedTotal(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const pollBatchStatus = async (id) => {
    const poll = async () => {
      try {
        const batch = await getBatch(id);
        setProgress(batch);

        if (batch.status === 'processing') {
          setTimeout(poll, 2000);
        } else if (batch.status === 'completed') {
          toast.success('Processing completed!');
          // Small delay to show completion message before redirect
          setTimeout(() => {
            navigate(`/reconciliation/${id}`);
          }, 1000);
        } else if (batch.status === 'failed') {
          setProcessing(false);
          toast.error('Processing failed');
        }
      } catch (error) {
        setProcessing(false);
        toast.error(error.message || 'Failed to get batch status');
      }
    };
    poll();
  };

  return (
    <div className="max-w-2xl mx-auto p-6">
      <h1 className="text-2xl font-bold mb-6">Upload Bank Transactions</h1>

      {!processing ? (
        <div>
          <div
            onDrop={handleDrop}
            onDragOver={handleDragOver}
            className="border-2 border-dashed border-gray-300 rounded-lg p-12 text-center cursor-pointer hover:border-blue-500"
            onClick={() => fileInputRef.current?.click()}
          >
            <input
              ref={fileInputRef}
              type="file"
              accept=".csv"
              onChange={handleFileSelect}
              className="hidden"
            />
            {file ? (
              <div>
                <p className="text-lg font-medium">{file.name}</p>
                <p className="text-sm text-gray-500 mt-2">
                  {(file.size / 1024).toFixed(2)} KB
                </p>
              </div>
            ) : (
              <div>
                <p className="text-lg">Drag and drop CSV file here</p>
                <p className="text-sm text-gray-500 mt-2">or click to browse</p>
                <p className="text-xs text-gray-400 mt-4">Max file size: 50MB</p>
              </div>
            )}
          </div>

          {file && (
            <div className="mt-4 flex gap-2">
              <button
                onClick={handleUpload}
                disabled={uploading}
                className="px-6 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
              >
                {uploading ? 'Uploading...' : 'Upload'}
              </button>
              <button
                onClick={handleReset}
                disabled={uploading}
                className="px-6 py-2 bg-gray-200 text-gray-700 rounded hover:bg-gray-300 disabled:opacity-50"
              >
                Clear
              </button>
            </div>
          )}
        </div>
      ) : (
        <div className="text-center">
          <LoadingSpinner size="lg" />
          <h2 className="text-xl font-semibold mt-4">Processing...</h2>
          {progress && (
            <div className="mt-4 space-y-2">
              <p>
                Processed {progress.processedCount} of{' '}
                {progress.totalTransactions || estimatedTotal || 'calculating...'} transactions
              </p>
              {(progress.totalTransactions || estimatedTotal) && (
                <div className="w-full bg-gray-200 rounded-full h-2">
                  <div
                    className="bg-blue-600 h-2 rounded-full transition-all"
                    style={{
                      width: `${progress.progressPercent || 
                        (estimatedTotal && estimatedTotal > 0 
                          ? (progress.processedCount / estimatedTotal * 100) 
                          : 0)}%`,
                    }}
                  />
                </div>
              )}
              <div className="grid grid-cols-3 gap-4 mt-6">
                <div>
                  <p className="text-sm text-gray-600">Auto-Matched</p>
                  <p className="text-lg font-semibold">{progress.counts.autoMatched}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600">Needs Review</p>
                  <p className="text-lg font-semibold">{progress.counts.needsReview}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600">Unmatched</p>
                  <p className="text-lg font-semibold">{progress.counts.unmatched}</p>
                </div>
              </div>
            </div>
          )}
          <div className="mt-6 flex gap-2 justify-center">
            <button
              onClick={handleReset}
              className="px-4 py-2 bg-gray-200 text-gray-700 rounded hover:bg-gray-300"
            >
              Upload New File
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

