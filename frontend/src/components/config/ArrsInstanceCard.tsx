import {
  Eye,
  EyeOff,
  Trash2,
} from "lucide-react";
import type { ArrsInstanceConfig, ArrsType } from "../../types/config";

interface ArrsInstanceCardProps {
  instance: ArrsInstanceConfig;
  type: ArrsType;
  index: number;
  isReadOnly: boolean;
  isApiKeyVisible: boolean;
  onToggleApiKey: () => void;
  onRemove: () => void;
  onInstanceChange: (
    field: keyof ArrsInstanceConfig,
    value: ArrsInstanceConfig[keyof ArrsInstanceConfig]
  ) => void;
}


export function ArrsInstanceCard({
  instance,
  type,
  index,
  isReadOnly,
  isApiKeyVisible,
  onToggleApiKey,
  onRemove,
  onInstanceChange,
}: ArrsInstanceCardProps) {
  const instanceKey = `${type}-${index}`;

  return (
    <div key={instanceKey} className="card bg-base-200">
      <div className="card-body p-4">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center space-x-3">
            <h4 className="font-semibold capitalize">{type} Instance</h4>
          </div>
          <div className="flex items-center space-x-2">
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
