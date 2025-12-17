import { useState, useEffect, useCallback } from 'react';
import { apiClient } from '../../api/client';
import { Play, Square, FolderOpen } from 'lucide-react';

export function FuseConfig() {
  const [status, setStatus] = useState<'stopped' | 'starting' | 'running' | 'error'>('stopped');
  const [path, setPath] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      const response = await apiClient.getFuseStatus();
      setStatus(response.status);
      if (response.path) {
        setPath(response.path);
      }
    } catch (err) {
      console.error('Failed to fetch FUSE status:', err);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    // Poll status every 5 seconds
    const interval = setInterval(fetchStatus, 5000);
    return () => clearInterval(interval);
  }, [fetchStatus]);

  const handleStart = async () => {
    setIsLoading(true);
    setError(null);
    try {
      await apiClient.startFuseMount(path);
      await fetchStatus();
    } catch (err) {
      setError('Failed to start mount. Check logs for details.');
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  };

  const handleStop = async () => {
    setIsLoading(true);
    setError(null);
    try {
      await apiClient.stopFuseMount();
      await fetchStatus();
    } catch (err) {
      setError('Failed to stop mount.');
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="card bg-base-100 shadow-xl">
      <div className="card-body">
        <h2 className="card-title flex items-center gap-2">
          <FolderOpen className="w-6 h-6" />
          Native FUSE Mount
        </h2>
        <p className="text-sm opacity-70">
          Mount the virtual filesystem directly to a local directory for high performance access.
        </p>

        <div className="form-control w-full max-w-lg mt-4">
          <label className="label">
            <span className="label-text">Mount Path</span>
          </label>
          <input
            type="text"
            placeholder="/mnt/altmount"
            className="input input-bordered w-full"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            disabled={status === 'running' || status === 'starting'}
          />
        </div>

        {error && (
          <div className="alert alert-error mt-4">
            <span>{error}</span>
          </div>
        )}

        <div className="card-actions justify-end mt-6 items-center">
          <div className="badge badge-lg mr-4 uppercase font-bold" 
            data-theme={status === 'running' ? 'success' : status === 'error' ? 'error' : 'neutral'}>
            {status}
          </div>

          {status === 'running' || status === 'starting' ? (
            <button 
              className="btn btn-error"
              onClick={handleStop}
              disabled={isLoading}
            >
              {isLoading ? <span className="loading loading-spinner"></span> : <Square className="w-4 h-4" />}
              Unmount
            </button>
          ) : (
            <button 
              className="btn btn-primary"
              onClick={handleStart}
              disabled={isLoading || !path}
            >
              {isLoading ? <span className="loading loading-spinner"></span> : <Play className="w-4 h-4" />}
              Mount
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
