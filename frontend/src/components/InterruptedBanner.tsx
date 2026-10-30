export interface InterruptedBannerProps {
  onResume: () => void;
  onDiscard: () => void;
}

export function InterruptedBanner({ onResume, onDiscard }: InterruptedBannerProps) {
  return (
    <div className="banner interrupted-banner" role="alert">
      <div>
        <strong>上次运行中断</strong>
        <div>
          继续会为未完成的工具调用补一条“结果未知”，并重新请求审批；放弃保留记录但不再继续。
        </div>
      </div>
      <div className="banner-actions">
        <button onClick={onResume}>继续</button>
        <button className="danger" onClick={onDiscard}>
          放弃
        </button>
      </div>
    </div>
  );
}
