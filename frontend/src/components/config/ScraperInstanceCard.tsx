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
import { useCancelScrape, useScraperInstanceInfo, useTriggerScrape } from "../../hooks/useScraper";
import type { ScraperInstanceConfig, ScraperType, ScrapeStatus } from "../../types/config";

interface ScraperInstanceCardProps {
  instance: ScraperInstanceConfig;
  type: ScraperType;
  index: number;
  isReadOnly: boolean;
  isApiKeyVisible: boolean;
  isExpanded: boolean;
  onToggleApiKey: () => void;
  onToggleExpanded: () => void;
  onRemove: () => void;
  onInstanceChange: (
    field: keyof ScraperInstanceConfig,
    value: ScraperInstanceConfig[keyof ScraperInstanceConfig]
  ) => void;
}

const getStatusIcon = (status: ScrapeStatus) => {
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
  const diffMs = end.getTime() - start.getTime();
  const minutes = Math.floor(diffMs / 60000);
  const seconds = Math.floor((diffMs % 60000) / 1000);
  return `${minutes}m ${seconds}s`;
};

export function ScraperInstanceCard({
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
}: ScraperInstanceCardProps) {
  // Configuration-first approach: always enable scraper instance info
  const scraperInfo = useScraperInstanceInfo(type, instance.name, true);
  const triggerScrape = useTriggerScrape();
  const cancelScrape = useCancelScrape();

  const instanceKey = `${type}-${index}`;

  return (
    <div key={instanceKey} className="card bg-base-200">
      <div className="card-body p-4">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center space-x-3">
            <h4 className="font-semibold capitalize">{type} Instance</h4>
            <div className="flex items-center space-x-2">
              {scraperInfo.status && getStatusIcon(scraperInfo.status.status)}
              {scraperInfo.status && (
                <span className="text-base-content/70 text-sm capitalize">
                  {scraperInfo.status.status}
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
            {scraperInfo.hasActiveStatus ? (
              <button
                type="button"
                className="btn btn-sm btn-warning"
                onClick={() => cancelScrape.mutate({ instanceType: type, instanceName: instance.name })}
                disabled={cancelScrape.isPending || scraperInfo.status?.status !== "running"}
              >
                <Square className="h-4 w-4" />
                Cancel
              </button>
            ) : (
              <button
                type="button"
                className="btn btn-sm btn-primary"
                onClick={() => triggerScrape.mutate({ instanceType: type, instanceName: instance.name })}
                disabled={triggerScrape.isPending || !instance.enabled}
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

            {scraperInfo.status && (scraperInfo.status.status === "running" || scraperInfo.status.status === "cancelling") ? (
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Current Status</div>
                  <div className="stat-value flex items-center space-x-2 text-lg">
                    {getStatusIcon(scraperInfo.status.status)}
                    <span className="capitalize">{scraperInfo.status.status}</span>
                  </div>
                  <div className="stat-desc">
                    Started: {new Date(scraperInfo.status.started_at).toLocaleString()}
                  </div>
                </div>

                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Progress</div>
                  <div className="stat-value text-lg">
                    {scraperInfo.status.processed_count}
                    {scraperInfo.status.total_items && ` / ${scraperInfo.status.total_items}`}
                  </div>
                  <div className="stat-desc">
                    {scraperInfo.status.error_count > 0 && (
                      <span className="text-error">{scraperInfo.status.error_count} errors</span>
                    )}
                  </div>
                </div>

                <div className="stat rounded-lg bg-base-200">
                  <div className="stat-title">Duration</div>
                  <div className="stat-value text-lg">{formatDuration(scraperInfo.status.started_at)}</div>
                  <div className="stat-desc">{scraperInfo.status.current_batch}</div>
                </div>

                {scraperInfo.status.total_items && scraperInfo.status.total_items > 0 && (
                  <div className="stat col-span-full rounded-lg bg-base-200">
                    <div className="stat-title">Progress Bar</div>
                    <progress
                      className="progress progress-primary w-full"
                      value={scraperInfo.status.processed_count}
                      max={scraperInfo.status.total_items}
                    />
                    <div className="stat-desc">
                      {Math.round(
                        (scraperInfo.status.processed_count / scraperInfo.status.total_items) * 100,
                      )}
                      % complete
                    </div>
                  </div>
                )}
              </div>
            ) : null}

            {/* Last Result */}
            {(scraperInfo.result || (scraperInfo.status && (scraperInfo.status.status === "completed" || scraperInfo.status.status === "failed"))) && (() => {
              // Use result data if available, otherwise fallback to status data for completed/failed scrapes
              const resultData = scraperInfo.result || (scraperInfo.status && (scraperInfo.status.status === "completed" || scraperInfo.status.status === "failed") 
                ? {
                    status: scraperInfo.status.status,
                    started_at: scraperInfo.status.started_at,
                    completed_at: new Date().toISOString(), // Current time as fallback
                    processed_count: scraperInfo.status.processed_count || 0,
                    error_count: scraperInfo.status.error_count || 0,
                    error_message: null
                  }
                : null);
              
              if (!resultData) return null;
              
              return (
                <div>
                  <h6 className="mb-2 font-medium text-base-content/70">
                    {scraperInfo.result ? "Last Scrape Result" : "Scrape Result"}
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
            {!scraperInfo.status && !scraperInfo.result && (
              <div className="py-8 text-center text-base-content/50">
                <Clock className="mx-auto mb-2 h-12 w-12" />
                <p>No scraping activity yet</p>
                <p className="text-sm">Click "Run" to start a manual scrape</p>
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
            <legend className="fieldset-legend">Scrape Interval (hours)</legend>
            <input
              type="number"
              className="input"
              value={instance.scrape_interval_hours}
              onChange={(e) =>
                onInstanceChange("scrape_interval_hours", Number.parseInt(e.target.value, 10) || 24)
              }
              min="1"
              max="168"
              disabled={isReadOnly}
            />
            <p className="label">How often to scrape for new files</p>
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

export default ScraperInstanceCard;
