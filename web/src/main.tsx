import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { Guide } from './Guide';
import './styles.css';

const path = window.location.pathname.replace(/\/+$/, '') || '/';
const page = path === '/guide' ? <Guide /> : <App />;
document.title = path === '/guide' ? '使い方 | PPLALE CMS' : 'PPLALE CMS';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    {page}
  </StrictMode>,
);
