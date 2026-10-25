// Original barcode status is a local read model, never another send right.
export function installScanRecoveryAPI({paths,schemas,object,str}) {
  schemas.ScanRecoveryStatus = object({
    state: str('经原来源、查询历史和撤销证明核验的本地状态。', {enum:['submitted','waiting','query_unresolved','stopped','completed','cancel_pending','cancelled','expired']}),
    queries: {type:'integer',minimum:0,maximum:6},
    maximum_queries: {type:'integer',const:6},
    last_provider_state: str('最近经认证的原通道状态，不将待支付解释为已入账。'),
    next_query_at: str('仅存在后续原查询时返回。',{format:'date-time'}),
    started_at: str(null,{format:'date-time'}),
    expires_at: str(null,{format:'date-time'}),
    requires_reconciliation: {type:'boolean',description:'需要核对原订单或撤销结果；不是退款授权或再次收款许可。'},
  },['state','queries','maximum_queries','started_at','expires_at','requires_reconciliation']);
  const detail = paths['/console/v1/{role}/orders/{trade}']?.get;
  if (!detail) throw new Error('Original console order detail contract required');
  detail.description += '\n\n新原付款码恢复来源可返回data.scan_recovery（ScanRecoveryStatus）；历史V0省略，不补授权。该GET只读取本地已验证历史，不调用支付机构，不泄露付款码、账户摘要或私有证明。cancel_pending表示原撤销已跨越永久发送边界但未得到可信最终应答，此时迟到SUCCESS不能入账；cancelled不表示商户收入增加。存在恢复来源时provider.can_close=false。原权限与商户归属不变，损坏来源不能伪装为普通订单。';
}
