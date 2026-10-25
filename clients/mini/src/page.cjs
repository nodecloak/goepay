'use strict';
const { createClient, describeLaunch } = require('./payment-client.cjs');

function createPage(provider, sdk, config) {
  let client, options, unloaded = false, clicked = false;
  return {
    data: { money: '', tradeNo: '', phase: 'idle', busy: false, canPay: false, message: '请从原订单打开支付页面。' },
    onLoad(input) {
      if (client || unloaded) return;
      try {
        const display = describeLaunch(provider, config, input);
        options = { money: input.money, url: input.url };
        client = createClient(provider, sdk, config, state => {
          if (unloaded) return;
          const busy = ['authorizing', 'exchanging', 'paying'].includes(state.phase);
          this.setData({ money: state.money, tradeNo: state.tradeNo, phase: state.phase, message: state.message, busy, canPay: false });
        });
        this.setData({ ...display, canPay: true, phase: 'ready', message: '请核对订单后确认支付。' });
      } catch (_) { this.setData({ canPay: false, phase: 'error', message: config && config.enabled === false ? '当前支付入口未配置，请联系商户。' : '支付入口不可用，请从原订单重新打开。' }); }
    },
    async onPay() {
      if (unloaded || clicked || !client || !this.data.canPay) return;
      clicked = true;
      await client.start(options);
      options = null;
    },
    onUnload() { unloaded = true; options = null; if (client) client.dispose(); },
  };
}
module.exports = { createPage };
