import { useState, useEffect, useCallback } from 'react';
import { apiClient } from '../../api/client';
import { Play, Square, FolderOpen, Save } from 'lucide-react';
import { useConfig, useUpdateConfigSection } from '../../hooks/useConfig';
import type { FuseConfig as FuseConfigType } from '../../types/config';

export function FuseConfig() {
  const { data: config } = useConfig();
  const updateConfig = useUpdateConfigSection();
  
  const [status, setStatus] = useState<'stopped' | 'starting' | 'running' | 'error'>('stopped');
  const [path, setPath] = useState('');
  const [formData, setFormData] = useState<Partial<FuseConfigType>>({
    allow_other: true,
    debug: false,
    attr_timeout_seconds: 1,
    entry_timeout_seconds: 1
  });
  
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Initialize form data from config
  useEffect(() => {
    if (config?.fuse) {
      setFormData(config.fuse);
      if (config.fuse.mount_path) {
        setPath(config.fuse.mount_path);
      }
    }
  }, [config]);

  const fetchStatus = useCallback(async () => {
    try {
      const response = await apiClient.getFuseStatus();
      setStatus(response.status);
      // Only update path from status if we don't have one set locally/from config yet?
      // Or maybe status path is the source of truth for running instance.
      if (response.path && response.status !== 'stopped') {
        setPath(response.path);
      }
    } catch (err) {
      console.error('Failed to fetch FUSE status:', err);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 5000);
    return () => clearInterval(interval);
  }, [fetchStatus]);

  const handleSave = async () => {
    try {
      await updateConfig.mutateAsync({
        section: 'fuse',
        config: { fuse: formData }
      });
      return true;
    } catch (err) {
      setError('Failed to save configuration.');
      return false;
    }
  };

  const handleStart = async () => {
    setIsLoading(true);
    setError(null);
    try {
      // Save config first to ensure options are persisted
      // We also update mount_path in formData for consistency
      const updatedData = { ...formData, mount_path: path };
      await updateConfig.mutateAsync({
        section: 'fuse',
        config: { fuse: updatedData }
      });

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

  const isRunning = status === 'running' || status === 'starting';

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

        <div className="divider"></div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="form-control w-full">
            <label className="label">
              <span className="label-text">Mount Path</span>
            </label>
            <input
              type="text"
              placeholder="/mnt/altmount"
              className="input input-bordered w-full"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              disabled={isRunning}
            />
          </div>

          <div className="form-control">
            <label className="label cursor-pointer">
              <span className="label-text">Allow Other Users</span>
              <input
                type="checkbox"
                className="toggle"
                checked={formData.allow_other ?? true}
                onChange={(e) => setFormData({...formData, allow_other: e.target.checked})}
                disabled={isRunning}
              />
            </label>
            <label className="label">
              <span className="label-text-alt opacity-70">Allow other users (like Docker/Plex) to access the mount</span>
            </label>
          </div>

          <div className="form-control">
            <label className="label">
              <span className="label-text">Attribute Timeout (seconds)</span>
            </label>
            <input
              type="number"
              className="input input-bordered"
              value={formData.attr_timeout_seconds ?? 1}
              onChange={(e) => setFormData({...formData, attr_timeout_seconds: parseInt(e.target.value) || 0})}
              disabled={isRunning}
            />
          </div>

          <div className="form-control">
            <label className="label">
              <span className="label-text">Entry Timeout (seconds)</span>
            </label>
            <input
              type="number"
              className="input input-bordered"
              value={formData.entry_timeout_seconds ?? 1}
              onChange={(e) => setFormData({...formData, entry_timeout_seconds: parseInt(e.target.value) || 0})}
              disabled={isRunning}
            />
          </div>

          <div className="form-control">
            <label className="label cursor-pointer">
              <span className="label-text">Debug Logging</span>
              <input
                type="checkbox"
                className="checkbox"
                checked={formData.debug ?? false}
                onChange={(e) => setFormData({...formData, debug: e.target.checked})}
                disabled={isRunning}
              />
            </label>
          </div>
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

          {!isRunning && (
             <button 
               className="btn btn-ghost mr-2"
               onClick={handleSave}
               disabled={updateConfig.isPending}
             >
               <Save className="w-4 h-4 mr-2" />
               Save Settings
             </button>
          )}

          {isRunning ? (
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