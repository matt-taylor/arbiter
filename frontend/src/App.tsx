import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from './contexts/ThemeProvider'
import { ToastProvider, ToastContainer } from './contexts/ToastContext'
import { ErrorBoundary } from './components/ErrorBoundary'
import PoliciesPage from './pages/PoliciesPage'
import EffectivePage from './pages/EffectivePage'
import TesterPage from './pages/TesterPage'

function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <ToastProvider>
          <BrowserRouter>
            <Routes>
              <Route path="/" element={<PoliciesPage />} />
              <Route path="/effective" element={<EffectivePage />} />
              <Route path="/tester" element={<TesterPage />} />
            </Routes>
            <ToastContainer />
          </BrowserRouter>
        </ToastProvider>
      </ThemeProvider>
    </ErrorBoundary>
  )
}

export default App
