import {
  CheckCircle,
  Clock,
  Eye,
  EyeOff,
  Loader2,
  Play,
  Square,
  Trash2,
  XCircle,
} from "lucide-react";
import { useCancelSync, useArrsInstanceInfo, useTriggerSync } from "../../hooks/useArrs";
import type { ArrsInstanceConfig, ArrsType, SyncStatus } from "../../types/config";

interface ArrsInstanceCardProps {
  instance: ArrsInstanceConfig;
  type: ArrsType;
  index: number;
  isReadOnly: boolean;
  isApiKeyVisible: boolean;
  isExpanded: boolean;
  onToggleApiKey: () => void;
  onToggleExpanded: () => void;
  onRemove: () => void;
  onInstanceChange: (
    field: keyof ArrsInstanceConfig,
    value: ArrsInstanceConfig[keyof ArrsInstanceConfig]
  ) => void;
}

const getStatusIcon = (status: SyncStatus) => {
  switch (status) {
    case "running":
      return <Loader2 className="h-4 w-4 animate-spin text-blue-500" />;
    case "cancelling":
      return <Square className="h-4 w-4 text-yellow-500" />;
    case "completed":
      return <CheckCircle className="h-4 w-4 text-green-500" />;
    case "failed":
      return <XCircle className="h-4 w-4 text-red-500" />;
    default:
      return <Clock className="h-4 w-4 text-gray-500" />;
  }
};

const formatDuration = (startTime: string, endTime?: string) => {
  const start = new Date(startTime);
  const end = endTime ? new Date(endTime) : new Date();
  
  // Validate dates and ensure start time is not in the future
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) {
    return "0m 0s";
  }
  
  const diffMs = end.getTime() - start.getTime();
  
  // Ensure duration is not negative (handle edge cases)
  if (diffMs < 0) {
    return "0m 0s";
  }
  
  const minutes = Math.floor(diffMs / 60000);
  const seconds = Math.floor((diffMs % 60000) / 1000);
  return `${minutes}m ${seconds}s`;
};

export function ArrsInstanceCard({
  instance,
  type,
  index,
  isReadOnly,
  isApiKeyVisible,
  isExpanded,
  onToggleApiKey,
  onToggleExpanded,
  onRemove,
  onInstanceChange,
}: ArrsInstanceCardProps) {
  // Configuration-first approach: always enable arrs instance info
  const arrsInfo = useArrsInstanceInfo(type, instance.name, true);
  const triggerSync = useTriggerSync();
  const cancelSync = useCancelSync();

  const instanceKey = `${type}-${index}`;

  return (
    <div key={instanceKey} className="card bg-base-200">
      <div className="card-body p-4">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center space-x-3">
            <h4 className="font-semibold capitalize">{type} Instance</h4>
            <div className="flex items-center space-x-2">
              {arrsInfo.status && getStatusIcon(arrsInfo.status.status)}
              {arrsInfo.status && (
                <span className="text-base-content/70 text-sm capitalize">
                  {arrsInfo.status.status}
                </span>
              )}
            </div>
          </div>
          <div className="flex items-center space-x-2">
            {/* Status toggle button */}
            <button type="button" className="btn btn-sm btn-ghost" onClick={onToggleExpanded}>
              {isExpanded ? "Hide Status" : "Show Status"}
            </button>
            {/* Control buttons */}
            {arrsInfo.hasActiveStatus ? (
              <button
                type="button"
                className="btn btn-sm btn-warning"
                onClick={() => cancelSync.mutate({ instanceType: type, instanceName: instance.name })}
                disabled={cancelSync.isPending || arrsInfo.status?.status !== "running"}
              >
                <Square className="h-4 w-4" />
                Cancel
              </button>
            ) : (
              <button
                type="button"
                className="btn btn-sm btn-primary"
                onClick={() => triggerSync.mutate({ instanceType: type, instanceName: instance.name })}
                disabled={triggerSync.isPending || !instance.enabled}
              >
                <Play className="h-4 w-4" />
                Run
              </button>
            )}
            <button
              type="button"
              className="btn btn-sm btn-error btn-outline"
              onClick={onRemove}
              disabled={isReadOnly}
            >
              <Trash2 className="h-4 w-4" />
              Remove
            </button>
          </div>
        </div>

        {/* Status Dashboard */}
        {isExpanded && (
          <div className="mb-4 space-y-4 rounded-lg bg-base-100 p-4">
            <h5 className="font-medium text-base-content/80">Status Dashboard</h5>

            {arrsInfo.status && (arrsInfo.status.status === "running" || arrsInfo.status.status === "cancelling") ? (
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Current Status</div>
                  <div className="stat-value flex items-center space-x-2 text-lg">
                    {getStatusIcon(arrsInfo.status.status)}
                    <span className="capitalize">{arrsInfo.status.status}</span>
                  </div>
                  <div className="stat-desc">
                    Started: {new Date(arrsInfo.status.started_at).toLocaleString()}
                  </div>
                </div>

                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Progress</div>
                  <div className="stat-value text-lg">
                    {arrsInfo.status.processed_count}
                    {arrsInfo.status.total_items && ` / ${arrsInfo.status.total_items}`}
                  </div>
                  <div className="stat-desc">
                    {arrsInfo.status.error_count > 0 && (
                      <span className="text-error">{arrsInfo.status.error_count} errors</span>
                    )}
                  </div>
                </div>

                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Duration</div>
                  <div className="stat-value text-lg">{formatDuration(arrsInfo.status.started_at)}</div>
                  <div className="stat-desc">{arrsInfo.status.current_batch}</div>
                </div>

                {arrsInfo.status.total_items && arrsInfo.status.total_items > 0 && (
                  <div className="stat col-span-full rounded-lg bg-base-200">
                    <div className="stat-title">Progress Bar</div>
                    <progress
                      className="progress progress-primary w-full"
                      value={arrsInfo.status.processed_count}
                      max={arrsInfo.status.total_items}
                    />
                    <div className="stat-desc">
                      {Math.round(
                        (arrsInfo.status.processed_count / arrsInfo.status.total_items) * 100,
                      )}
                      % complete
                    </div>
                  </div>
                )}
              </div>
            ) : null}

            {/* Last Result */}
            {(arrsInfo.result || (arrsInfo.status && (arrsInfo.status.status === "completed" || arrsInfo.status.status === "failed"))) && (() => {
              // Use result data if available, otherwise fallback to status data for completed/failed syncs
              const resultData = arrsInfo.result || (arrsInfo.status && (arrsInfo.status.status === "completed" || arrsInfo.status.status === "failed") 
                ? {
                    status: arrsInfo.status.status,
                    started_at: arrsInfo.status.started_at,
                    completed_at: new Date().toISOString(), // Current time as fallback
                    processed_count: arrsInfo.status.processed_count || 0,
                    error_count: arrsInfo.status.error_count || 0,
                    error_message: null
                  }
                : null);
              
              if (!resultData) return null;
              
              return (
                <div>
                  <h6 className="mb-2 font-medium text-base-content/70">
                    {arrsInfo.result ? "Last Sync Result" : "Sync Result"}
                  </h6>
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
                    <div className="stat rounded-lg bg-base-200">
                      <div className="stat-title">Status</div>
                      <div className="stat-value flex items-center space-x-2 text-lg">
                        {getStatusIcon(resultData.status)}
                        <span className="capitalize">{resultData.status}</span>
                      </div>
                    </div>

                    <div className="stat rounded-lg bg-base-200">
                      <div className="stat-title">Processed</div>
                      <div className="stat-value text-lg">{resultData.processed_count}</div>
                    </div>

                    <div className="stat rounded-lg bg-base-200">
                      <div className="stat-title">Errors</div>
                      <div className="stat-value text-error text-lg">{resultData.error_count}</div>
                    </div>

                    <div className="stat rounded-lg bg-base-200">
                      <div className="stat-title">Duration</div>
                      <div className="stat-value text-lg">
                        {formatDuration(resultData.started_at, resultData.completed_at)}
                      </div>
                      <div className="stat-desc">
                        Completed: {new Date(resultData.completed_at).toLocaleString()}
                      </div>
                    </div>

                    {resultData.error_message && (
                      <div className="stat col-span-full rounded-lg bg-error/10">
                        <div className="stat-title text-error">Error Message</div>
                        <div className="stat-desc text-error">{resultData.error_message}</div>
                      </div>
                    )}
                  </div>
                </div>
              );
            })()}

            {/* No Status Available */}
            {!arrsInfo.status && !arrsInfo.result && (
              <div className="py-8 text-center text-base-content/50">
                <Clock className="mx-auto mb-2 h-12 w-12" />
                <p>No scraping activity yet</p>
                <p className="text-sm">Click "Run" to start a manual sync</p>
              </div>
            )}
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <fieldset className="fieldset">
            <legend className="fieldset-legend">Instance Name</legend>
            <input
              type="text"
              className="input"
              value={instance.name}
              onChange={(e) => onInstanceChange("name", e.target.value)}
              placeholder="My Radarr/Sonarr"
              disabled={isReadOnly}
            />
          </fieldset>

          <fieldset className="fieldset">
            <legend className="fieldset-legend">URL</legend>
            <input
              type="url"
              className="input"
              value={instance.url}
              onChange={(e) => onInstanceChange("url", e.target.value)}
              placeholder="http://localhost:7878"
              disabled={isReadOnly}
            />
          </fieldset>

          <fieldset className="fieldset">
            <legend className="fieldset-legend">API Key</legend>
            <div className="flex">
              <input
                type={isApiKeyVisible ? "text" : "password"}
                className="input flex-1"
                value={instance.api_key}
                onChange={(e) => onInstanceChange("api_key", e.target.value)}
                placeholder="API key from settings"
                disabled={isReadOnly}
              />
              <button
                type="button"
                className="btn btn-outline ml-2"
                onClick={onToggleApiKey}
                disabled={isReadOnly}
              >
                {isApiKeyVisible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </fieldset>

          <fieldset className="fieldset">
            <legend className="fieldset-legend">Sync Interval (hours)</legend>
            <input
              type="number"
              className="input"
              value={instance.sync_interval_hours}
              onChange={(e) =>
                onInstanceChange("sync_interval_hours", Number.parseInt(e.target.value, 10) || 24)
              }
              min="1"
              max="168"
              disabled={isReadOnly}
            />
            <p className="label">How often to sync for new files</p>
          </fieldset>
        </div>

        <div className="mt-4">
          <label className="label cursor-pointer">
            <span className="label-text">Enable this instance</span>
            <input
              type="checkbox"
              className="checkbox"
              checked={instance.enabled}
              onChange={(e) => onInstanceChange("enabled", e.target.checked)}
              disabled={isReadOnly}
            />
          </label>
        </div>
      </div>
    </div>
  );
}

export default ArrsInstanceCard;
