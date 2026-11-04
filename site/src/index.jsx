import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { BrowserRouter as Router } from 'react-router-dom';


function MyApp() {
  return (
    <Router>
      <App />
    </Router>
  );
}


ReactDOM.createRoot(document.getElementById('root')).render(
  <MyApp />
)
