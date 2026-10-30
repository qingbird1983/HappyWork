import type { PausedPayload } from "../types";
import { SafeText } from "../render";

export interface PausedBannerProps {
  paused: PausedPayload;
  onResume: () => void;
  onStop: () => void;
}

export function PausedBanner({ paused, onResume, onStop }: PausedBannerProps) {
  const isBudget = paused.reason === "budget";
  const reason = isBudget ? "预算暂停" : "审批超时";
  return (
    <div className="banner paused-banner" role="alert">
      <div>
        <strong>{reason}</strong>
        <div>
          <SafeText text={paused.detail} />
        </div>
      </div>
      <div className="banner-actions">
        <button onClick={onResume}>{isBudget ? "继续（一次宽限）" : "继续（重新审批）"}</button>
        <button className="danger" onClick={onStop}>
          停止
        </button>
      </div>
    </div>
  );
}
