import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { BrowserRouter as Router } from 'react-router-dom';
import { StyledEngineProvider } from '@mui/material/styles';


function MyApp() {
  return (
    <Router>
      <App />
    </Router>
  );
}


ReactDOM.createRoot(document.getElementById('root')).render(
    <StyledEngineProvider injectFirst>
      <MyApp />
    </StyledEngineProvider>
)