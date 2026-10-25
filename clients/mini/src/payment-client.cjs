'use strict';

// No SDK globals, storage, logging, cookies or payment keys. The registry lives
// only in this mini-program process; the server remains the one-use authority.
const claims = new Set();
const terminal = '请通过原订单查单核实结果；请勿重复发起支付。';
const positiveID = /^[1-9][0-9]{0,18}$/;
const safeTrade = /^[A-Za-z0-9_-]{1,64}$/;
const safeCode = /^[\x21-\x7e]{1,4096}$/;
const originPattern = /^https:\/\/(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z](?:[a-z0-9-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$/;
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
function invalid() { throw new Error('invalid mini payment context'); }
function parseLaunch(provider, config, options) {
  if (!record(config) || config.enabled !== true || !originPattern.test(config.origin) || typeof config.appId !== 'string' || !/^[A-Za-z0-9_-]{1,64}$/.test(config.appId) || !Array.isArray(config.channelIDs) || !config.channelIDs.length || config.channelIDs.some(x => typeof x !== 'string' || !positiveID.test(x))) invalid();
  if (!record(options) || typeof options.url !== 'string' || options.url.length > 2048 || typeof options.money !== 'string' || !/^(?:0|[1-9][0-9]{0,9})\.[0-9]{2}$/.test(options.money) || /^0\.00$/.test(options.money)) invalid();
  let url = options.url;
  if (/^https%3a%2f%2f/i.test(url)) { try { url = decodeURIComponent(url); } catch (_) { invalid(); } }
  const prefix = config.origin + '/oauth/mini/complete/';
  if (!url.startsWith(prefix)) invalid();
  const parts = url.slice(prefix.length).match(/^(wxpay|alipay)\/([1-9][0-9]{0,18})\/([A-Za-z0-9_-]{1,64})\/\?state=([A-Za-z0-9_-]{32,128})$/);
  if (!parts || parts[1] !== provider || !config.channelIDs.includes(parts[2])) invalid();
  return Object.freeze({ url, origin: config.origin, appId: config.appId, provider, channel: parts[2], trade: parts[3], state: parts[4], money: options.money });
}
function checkoutURL(value, context) {
  if (typeof value !== 'string' || value.length > 2048) invalid();
  const prefix = context.origin + '/checkout/' + context.trade + '?';
  if (!value.startsWith(prefix)) invalid();
  const query = value.slice(prefix.length);
  if (!/^(?:expires=[1-9][0-9]{0,11}&token=[A-Za-z0-9_-]{43}|token=[A-Za-z0-9_-]{43}&expires=[1-9][0-9]{0,11})$/.test(query)) invalid();
  return value;
}
function paymentMaterial(provider, attempt, context) {
  if (provider === 'alipay') {
    if (typeof attempt.material !== 'string' || !/^[0-9]{16,64}$/.test(attempt.material) || attempt.material !== attempt.provider_trade_no) invalid();
    return { tradeNO: attempt.material };
  }
  if (typeof attempt.material !== 'string' || attempt.material.length > 4096) invalid();
  let input; try { input = JSON.parse(attempt.material); } catch (_) { invalid(); }
  const keys = ['appId', 'timeStamp', 'nonceStr', 'package', 'signType', 'paySign'];
  if (!record(input) || Object.keys(input).length !== keys.length || keys.some(k => !own(input, k) || typeof input[k] !== 'string') || input.appId !== context.appId || !/^[1-9][0-9]{0,11}$/.test(input.timeStamp) || !/^[A-Za-z0-9_-]{1,32}$/.test(input.nonceStr) || !/^prepay_id=[A-Za-z0-9_-]{1,128}$/.test(input.package) || !['MD5', 'HMAC-SHA256', 'RSA'].includes(input.signType)) invalid();
  if (input.signType === 'MD5' && !/^[A-Fa-f0-9]{32}$/.test(input.paySign) || input.signType === 'HMAC-SHA256' && !/^[A-Fa-f0-9]{64}$/.test(input.paySign) || input.signType === 'RSA' && !/^[A-Za-z0-9+/]{64,1024}={0,2}$/.test(input.paySign)) invalid();
  return { timeStamp: input.timeStamp, nonceStr: input.nonceStr, package: input.package, signType: input.signType, paySign: input.paySign };
}
function createClient(provider, sdk, config, onState, timerAPI) {
  const timers = timerAPI || { setTimeout, clearTimeout };
  let started = false, disposed = false, cancelPending = null, context = null, checkout = '';
  // Clone public configuration so callers cannot change a running intent.
  const trusted = record(config) ? Object.assign({}, config, { channelIDs: Array.isArray(config.channelIDs) ? config.channelIDs.slice() : [] }) : null;
  function show(phase, message) {
    if (!disposed) onState(Object.freeze({ phase, message, money: context ? context.money : '', tradeNo: context ? context.trade : '', checkoutURL: checkout }));
  }
  function active() { if (disposed) throw new Error('disposed'); }
  function call(method, fields, milliseconds) {
    return new Promise((resolve, reject) => {
      let settled = false, task, timer;
      function finish(error, value, abort) {
        if (settled) return;
        settled = true; timers.clearTimeout(timer); cancelPending = null;
        if (abort && task && typeof task.abort === 'function') { try { task.abort(); } catch (_) {} }
        if (error) {
          const failure = new Error('mini SDK operation unavailable');
          failure.cancelled = method === 'requestPayment' && record(value) && typeof value.errMsg === 'string' && /^requestPayment:fail cancel$/.test(value.errMsg);
          reject(failure);
        } else resolve(value);
      }
      cancelPending = () => finish(true, null, true);
      timer = timers.setTimeout(() => finish(true, null, true), milliseconds);
      try {
        if (!sdk || typeof sdk[method] !== 'function') return finish(true);
        task = sdk[method](Object.assign({}, fields, { success: value => finish(false, value), fail: value => finish(true, value) }));
      } catch (_) { finish(true); }
    });
  }
  async function start(options) {
    if (started || disposed) return;
    started = true;
    let exchangeStarted = false;
    try {
      if (provider !== 'wxpay' && provider !== 'alipay') invalid();
      context = parseLaunch(provider, trusted, options);
      const identity = provider === 'wxpay' ? sdk.getAccountInfoSync() : sdk.getAppIdSync();
      const actual = provider === 'wxpay' ? identity && identity.miniProgram && identity.miniProgram.appId : identity && identity.appId;
      if (actual !== context.appId) invalid();
      const claim = context.origin + '|' + provider + '|' + context.state;
      if (claims.has(claim) || claims.size >= 200) invalid();
      claims.add(claim);
      show('authorizing', '正在获取支付授权');
      const identityResult = await call(provider === 'wxpay' ? 'login' : 'getAuthCode', provider === 'wxpay' ? { timeout: 10000 } : { scopes: ['auth_base'] }, 15000);
      active();
      const code = provider === 'wxpay' ? identityResult && identityResult.code : identityResult && identityResult.authCode;
      if (typeof code !== 'string' || !safeCode.test(code)) invalid();
      exchangeStarted = true; show('exchanging', '正在准备原订单支付');
      const fields = { url: context.url, method: 'POST', data: { code }, timeout: 15000 };
      fields[provider === 'wxpay' ? 'header' : 'headers'] = { 'Content-Type': 'application/json' };
      if (provider === 'alipay') fields.dataType = 'json';
      const http = await call('request', fields, 20000);
      active();
      const status = provider === 'wxpay' ? http && http.statusCode : http && http.status;
      const body = http && http.data;
      if (status !== 200 || !record(body) || body.code !== 0 || !record(body.data) || body.data.trade_no !== context.trade) invalid();
      const data = body.data, attempt = data.attempt;
      const validatedCheckout = checkoutURL(data.checkout_url, context);
      if (!record(attempt) || typeof attempt.id !== 'string' || !positiveID.test(attempt.id) || attempt.trade_no !== context.trade || attempt.channel_id !== context.channel || attempt.method !== (provider === 'wxpay' ? 'jsapi' : 'mini')) invalid();
      if (attempt.state !== 'pending') { checkout = validatedCheckout; show('verification_required', terminal); return; }
      if (attempt.material_type !== 'jsapi') invalid();
      const material = paymentMaterial(provider, attempt, context);
      checkout = validatedCheckout;
      show('paying', '请在支付窗口确认');
      let result;
      try { result = await call(provider === 'wxpay' ? 'requestPayment' : 'tradePay', material, 180000); }
      catch (failure) { active(); show(failure.cancelled ? 'cancelled' : 'verification_required', failure.cancelled ? '已取消支付。' + terminal : terminal); return; }
      active();
      if (provider === 'alipay' && String(result && result.resultCode) === '6001') { show('cancelled', '已取消支付。' + terminal); return; }
      show('verification_required', terminal);
    } catch (_) { show(exchangeStarted ? 'verification_required' : 'error', exchangeStarted ? terminal : '支付入口不可用，请返回原订单重新打开。'); }
  }
  return { start, dispose() { if (disposed) return; disposed = true; if (cancelPending) cancelPending(); } };
}
function describeLaunch(provider, config, options) {
  if (provider !== 'wxpay' && provider !== 'alipay') invalid();
  const context = parseLaunch(provider, config, options);
  return { money: context.money, tradeNo: context.trade };
}
module.exports = { createClient, describeLaunch };
