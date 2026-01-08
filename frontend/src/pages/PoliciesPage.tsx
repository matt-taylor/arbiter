import { useState, useEffect } from 'react'
import type React from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, Edit, Trash2, Search, Sun, Moon, Monitor, TestTube } from 'lucide-react'
import { policiesApi } from '@/lib/api'
import type { HostPolicy, CreatePolicyRequest } from '@/lib/types'
import { useToast } from '@/hooks/useToast'
import { useTheme } from '@/hooks/useTheme'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Badge from '@/components/ui/badge'
import Dialog from '@/components/ui/dialog'
import Loading from '@/components/ui/loading'

export default function PoliciesPage() {
  const navigate = useNavigate()
  const [policies, setPolicies] = useState<HostPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [searchTerm, setSearchTerm] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<HostPolicy | null>(null)
  const [formData, setFormData] = useState<CreatePolicyRequest>({
    host: '',
    killswitch_required: false,
    gatekeeper_required: false,
    notes: '',
  })
  const { success, error } = useToast()
  const { theme, setTheme } = useTheme()

  useEffect(() => {
    loadPolicies()
  }, [])

  const loadPolicies = async () => {
    try {
      setLoading(true)
      const data = await policiesApi.list()
      setPolicies(data)
    } catch (err: any) {
      console.error('Error loading policies:', err)
      const message = err?.response?.data?.error || err?.response?.data?.message || err?.message || 'Failed to load policies'
      error(message)
    } finally {
      setLoading(false)
    }
  }

  const handleCreate = () => {
    setEditingPolicy(null)
    setFormData({
      host: '',
      killswitch_required: false,
      gatekeeper_required: false,
      notes: '',
    })
    setDialogOpen(true)
  }

  const handleEdit = (policy: HostPolicy) => {
    setEditingPolicy(policy)
    setFormData({
      host: policy.host,
      killswitch_required: policy.killswitch_required,
      gatekeeper_required: policy.gatekeeper_required,
      notes: policy.notes || '',
    })
    setDialogOpen(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('Are you sure you want to delete this policy?')) return

    try {
      await policiesApi.delete(id)
      success('Policy deleted successfully')
      await loadPolicies()
    } catch (err: any) {
      console.error('Error deleting policy:', err)
      const message = err?.response?.data?.error || err?.response?.data?.message || err?.message || 'Failed to delete policy'
      error(message)
    }
  }

  const handleSubmit = async () => {
    // Validate form
    if (!formData.host || !formData.host.trim()) {
      error('Host is required')
      return
    }

    try {
      if (editingPolicy) {
        await policiesApi.update(editingPolicy.id, formData)
        success('Policy updated successfully')
      } else {
        await policiesApi.create(formData)
        success('Policy created successfully')
      }
      setDialogOpen(false)
      // Reset form
      setFormData({
        host: '',
        killswitch_required: false,
        gatekeeper_required: false,
        notes: '',
      })
      await loadPolicies()
    } catch (err: any) {
      console.error('Error saving policy:', err)
      let message = 'Failed to save policy'

      if (err?.response) {
        const data = err.response.data
        if (typeof data === 'string') {
          message = data
        } else if (data?.error) {
          message = data.error
        } else if (data?.message) {
          message = data.message
        } else if (err.response.status === 409) {
          message = 'Policy already exists for this host'
        } else if (err.response.status === 400) {
          message = typeof data === 'string' ? data : 'Invalid request'
        }
      } else if (err?.message) {
        message = err.message
      }

      error(message)
      // Don't close dialog on error so user can fix and retry
    }
  }

  const filteredPolicies = policies.filter((policy) =>
    policy.host.toLowerCase().includes(searchTerm.toLowerCase())
  )

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b bg-card">
        <div className="container mx-auto px-4 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <img
              src="/favicon.svg"
              alt="Arbiter"
              className="w-8 h-8"
            />
            <h1 className="text-2xl font-bold">Arbiter</h1>
            {/* Theme Selector */}
            <div className="flex gap-1 border rounded-md p-1 bg-background ml-2">
              <Button
                variant={theme === 'light' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('light')}
                className="h-8 w-8 p-0"
                aria-label="Light theme"
                title="Light"
              >
                <Sun className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'dark' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('dark')}
                className="h-8 w-8 p-0"
                aria-label="Dark theme"
                title="Dark"
              >
                <Moon className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'system' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('system')}
                className="h-8 w-8 p-0"
                aria-label="System theme"
                title="System"
              >
                <Monitor className="h-4 w-4" />
              </Button>
            </div>
          </div>
          <div className="flex items-center gap-3">
            {/* Tester Button */}
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigate('/tester')}
            >
              <TestTube className="h-4 w-4 mr-2" />
              Tester
            </Button>
            <Button onClick={handleCreate}>
              <Plus className="h-4 w-4 mr-2" />
              Create Policy
            </Button>
          </div>
        </div>
      </header>

      <div className="container mx-auto px-4 py-6 max-w-6xl">

      <div className="mb-4">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search policies..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10"
          />
        </div>
      </div>

      {loading ? (
        <Loading />
      ) : (
        <div className="bg-card rounded-lg border">
          {filteredPolicies.length === 0 ? (
            <div className="p-8 text-center text-muted-foreground">
              {searchTerm ? 'No policies match your search' : 'No policies yet. Create one to get started.'}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b">
                    <th className="text-left p-4 font-semibold">Host</th>
                    <th className="text-left p-4 font-semibold">Killswitch</th>
                    <th className="text-left p-4 font-semibold">Gatekeeper</th>
                    <th className="text-left p-4 font-semibold">Notes</th>
                    <th className="text-right p-4 font-semibold">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredPolicies.map((policy) => (
                    <tr key={policy.id} className="border-b hover:bg-accent/50">
                      <td className="p-4 font-mono text-sm">{policy.host}</td>
                      <td className="p-4">
                        {policy.killswitch_required ? (
                          <Badge variant="success">Required</Badge>
                        ) : (
                          <Badge variant="default">Not Required</Badge>
                        )}
                      </td>
                      <td className="p-4">
                        {policy.gatekeeper_required ? (
                          <Badge variant="success">Required</Badge>
                        ) : (
                          <Badge variant="default">Not Required</Badge>
                        )}
                      </td>
                      <td className="p-4 text-sm text-muted-foreground">
                        {policy.notes || '-'}
                      </td>
                      <td className="p-4">
                        <div className="flex justify-end gap-2">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleEdit(policy)}
                          >
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(policy.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      <Dialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        title={editingPolicy ? 'Edit Policy' : 'Create Policy'}
        footer={
          <>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => handleSubmit()}>
              {editingPolicy ? 'Update' : 'Create'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Host</label>
            <Input
              value={formData.host}
              onChange={(e) => setFormData({ ...formData, host: e.target.value.toLowerCase() })}
              placeholder="example.com"
              disabled={!!editingPolicy}
            />
          </div>
          <div className="flex items-center gap-4">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.killswitch_required}
                onChange={(e) => setFormData({ ...formData, killswitch_required: e.target.checked })}
                className="h-4 w-4"
              />
              <span className="text-sm">Killswitch Required</span>
            </label>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.gatekeeper_required}
                onChange={(e) => setFormData({ ...formData, gatekeeper_required: e.target.checked })}
                className="h-4 w-4"
              />
              <span className="text-sm">Gatekeeper Required</span>
            </label>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2">Notes</label>
            <textarea
              value={formData.notes || ''}
              onChange={(e) => setFormData({ ...formData, notes: e.target.value })}
              placeholder="Optional notes..."
              className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            />
          </div>
        </div>
      </Dialog>
      </div>
    </div>
  )
}
