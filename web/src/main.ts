import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import "./style.css";
import { capturePromotionPrefill } from "./referral";
import { installDocumentBoundary202 } from "./document-boundary202";
// Strip bearer prefill before constructing router history or issuing requests.
capturePromotionPrefill();
const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: "/:pathMatch(.*)*", component: { template: "<span />" } }],
});
installDocumentBoundary202(router);
const app = createApp(App).use(router);
// Resolve the initial merchant path before consuming the registration prefill.
void router.isReady().then(() => app.mount("#app"));
