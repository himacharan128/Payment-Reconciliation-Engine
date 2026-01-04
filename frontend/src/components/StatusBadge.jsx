import { getStatusLabel, getStatusColor } from '../lib/utils';

export default function StatusBadge({ status }) {
  const color = getStatusColor(status);
  const label = getStatusLabel(status);

  const colorClasses = {
    gray: 'bg-gray-100 text-gray-800',
    green: 'bg-green-100 text-green-800',
    yellow: 'bg-yellow-100 text-yellow-800',
    red: 'bg-red-100 text-red-800',
    blue: 'bg-blue-100 text-blue-800',
  };

  return (
    <span className={`px-2 py-1 rounded text-xs font-medium ${colorClasses[color]}`}>
      {label}
    </span>
  );
}

