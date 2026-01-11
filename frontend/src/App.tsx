import { useEffect, useRef } from 'react'
import { BrowserRouter, Routes, Route, useNavigate, useLocation } from 'react-router-dom'
import { ThemeProvider } from './contexts/ThemeProvider'
import { ToastProvider, ToastContainer } from './contexts/ToastContext'
import { ErrorBoundary } from './components/ErrorBoundary'
import { useToast } from './hooks/useToast'
import PoliciesPage from './pages/PoliciesPage'
import EffectivePage from './pages/EffectivePage'
import TesterPage from './pages/TesterPage'

function NotFound() {
  const navigate = useNavigate()
  const location = useLocation()
  const { error: showError } = useToast()
  const handledPathname = useRef<string | null>(null)

  useEffect(() => {
    // Prevent duplicate toasts and redirects (especially in React StrictMode)
    if (handledPathname.current !== location.pathname) {
      handledPathname.current = location.pathname
      showError('That page does not exist')
      navigate('/', { replace: true })
    }
  }, [navigate, showError, location.pathname])

  return null
}

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
              {/* Catch-all route: redirect non-existent routes to root */}
              <Route path="*" element={<NotFound />} />
            </Routes>
            <ToastContainer />
          </BrowserRouter>
        </ToastProvider>
      </ThemeProvider>
    </ErrorBoundary>
  )
}

export default App
