import { createRouter, createWebHistory } from '@ionic/vue-router';
import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/computers' },
  { path: '/computers', name: 'Computers', component: () => import('../views/ComputersPage.vue') },
  { path: '/pair', name: 'Pair', component: () => import('../views/PairPage.vue') },
  {
    path: '/computers/:computerId',
    name: 'Sessions',
    component: () => import('../views/SessionsPage.vue'),
  },
  {
    path: '/computers/:computerId/sessions/:sessionId',
    name: 'Terminal',
    component: () => import('../views/TerminalPage.vue'),
  },
];

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
});

export default router;
