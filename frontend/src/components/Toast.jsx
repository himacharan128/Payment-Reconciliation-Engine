import { useState, useEffect } from 'react';

let toastId = 0;
const toasts = [];
const listeners = [];

export const toast = {
  success: (message) => addToast('success', message),
  error: (message) => addToast('error', message),
  info: (message) => addToast('info', message),
};

function addToast(type, message) {
  const id = toastId++;
  toasts.push({ id, type, message });
  listeners.forEach(listener => listener([...toasts]));
  
  setTimeout(() => {
    removeToast(id);
  }, 5000);
}

function removeToast(id) {
  const index = toasts.findIndex(t => t.id === id);
  if (index > -1) {
    toasts.splice(index, 1);
    listeners.forEach(listener => listener([...toasts]));
  }
}

export function ToastContainer() {
  const [toastList, setToastList] = useState([]);

  useEffect(() => {
    listeners.push(setToastList);
    return () => {
      const index = listeners.indexOf(setToastList);
      if (index > -1) listeners.splice(index, 1);
    };
  }, []);

  const typeStyles = {
    success: 'bg-green-500',
    error: 'bg-red-500',
    info: 'bg-blue-500',
  };

  return (
    <div className="fixed top-4 right-4 z-50 space-y-2">
      {toastList.map((toast) => (
        <div
          key={toast.id}
          className={`${typeStyles[toast.type]} text-white px-4 py-2 rounded shadow-lg flex items-center gap-2 min-w-[300px]`}
        >
          <span>{toast.message}</span>
          <button
            onClick={() => removeToast(toast.id)}
            className="ml-auto text-white hover:text-gray-200"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}

