import { render } from 'preact';
import App from './App';
import '@picocss/pico/css/pico.css';
import './style.css';

const container = document.getElementById('root');
if (container) {
  render(<App />, container);
}
