import { useState, useEffect, useMemo } from 'react'
import { Plus, Edit, Trash2, Search, ChevronLeft, ChevronRight } from 'lucide-react'
import { policiesApi } from '@/lib/api'
import type { HostPolicy, CreatePolicyRequest } from '@/lib/types'
import { useToast } from '@/hooks/useToast'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Badge from '@/components/ui/badge'
import Dialog from '@/components/ui/dialog'
import Loading from '@/components/ui/loading'
import Tooltip from '@/components/ui/tooltip'
import { TableCard, TableCardRow, TableCardActions } from '@/components/TableCard'

export default function PoliciesPage() {
  const [policies, setPolicies] = useState<HostPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [searchTerm, setSearchTerm] = useState('')
  const [currentPage, setCurrentPage] = useState(1)
  const [itemsPerPage] = useState(10)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<HostPolicy | null>(null)
  const [formData, setFormData] = useState<CreatePolicyRequest>({
    host: '',
    killswitch_required: false,
    gatekeeper_required: false,
    notes: '',
  })
  const { success, error } = useToast()

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
    if (policy.managed) {
      const packName = policy.managed_pack || 'policy pack'
      error(`This policy is managed by ${packName}. Edit the YAML file and re-apply the pack.`)
      return
    }
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
    const policy = policies.find(p => p.id === id)
    if (policy?.managed) {
      const packName = policy.managed_pack || 'policy pack'
      error(`This policy is managed by ${packName}. Edit the YAML file and re-apply the pack.`)
      return
    }

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
          // Check if it's a managed policy error
          if (typeof data === 'string' && data.includes('managed by pack')) {
            message = data
          } else {
            message = 'Policy already exists for this host'
          }
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

  const filteredPolicies = useMemo(() => {
    const filtered = policies.filter((policy) =>
      policy.host.toLowerCase().includes(searchTerm.toLowerCase())
    )
    // Sort lexicographically by host to ensure consistent ordering
    return filtered.sort((a, b) => a.host.localeCompare(b.host))
  }, [policies, searchTerm])

  // Pagination calculations
  const totalPages = Math.ceil(filteredPolicies.length / itemsPerPage)
  const startIndex = (currentPage - 1) * itemsPerPage
  const endIndex = startIndex + itemsPerPage
  const paginatedPolicies = filteredPolicies.slice(startIndex, endIndex)

  // Reset to page 1 when search term changes
  useEffect(() => {
    setCurrentPage(1)
  }, [searchTerm])

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Policies</h1>
        <Button onClick={handleCreate} size="sm">
          <Plus className="h-4 w-4 mr-2" />
          Create Policy
        </Button>
      </div>

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
              <>
                {/* Desktop Table */}
                <div className="hidden md:block overflow-x-auto overflow-y-auto max-h-[calc(100vh-280px)]">
                  <table className="w-full">
                    <thead className="sticky top-0 z-10 bg-card">
                      <tr className="border-b">
                        <th className="text-left p-4 font-semibold bg-card">Host</th>
                        <th className="text-left p-4 font-semibold bg-card">Managed</th>
                        <th className="text-left p-4 font-semibold bg-card">Killswitch</th>
                        <th className="text-left p-4 font-semibold bg-card">Gatekeeper</th>
                        <th className="text-left p-4 font-semibold bg-card">Notes</th>
                        <th className="text-right p-4 font-semibold bg-card">Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {paginatedPolicies.map((policy) => {
                        const isManaged = policy.managed === true
                        return (
                          <tr key={policy.id} className="border-b hover:bg-accent/50">
                            <td className="p-4 font-mono text-sm">{policy.host}</td>
                            <td className="p-4">
                              {isManaged ? (
                                <div className="flex flex-col gap-1">
                                  <Badge variant="default">Managed</Badge>
                                  {policy.managed_pack && (
                                    <span className="text-xs text-muted-foreground">
                                      {policy.managed_pack}
                                      {policy.managed_version && ` v${policy.managed_version}`}
                                    </span>
                                  )}
                                </div>
                              ) : (
                                <span className="text-muted-foreground text-sm">-</span>
                              )}
                            </td>
                            <td className="p-4">
                              {policy.killswitch_required ? (
                                <Tooltip content="Killswitch service is required for this host. Requests must pass through Killswitch before proceeding.">
                                  <Badge variant="success">Required</Badge>
                                </Tooltip>
                              ) : (
                                <Tooltip content="Killswitch service is not required for this host. Requests can proceed without Killswitch checks.">
                                  <Badge variant="danger">－</Badge>
                                </Tooltip>
                              )}
                            </td>
                            <td className="p-4">
                              {policy.gatekeeper_required ? (
                                <Tooltip content="Gatekeeper service is required for this host. Requests must pass through Gatekeeper authorization before proceeding.">
                                  <Badge variant="success">Required</Badge>
                                </Tooltip>
                              ) : (
                                <Tooltip content="Gatekeeper service is not required for this host. Requests can proceed without Gatekeeper authorization checks.">
                                  <Badge variant="danger">－</Badge>
                                </Tooltip>
                              )}
                            </td>
                            <td className="p-4 text-sm text-muted-foreground">
                              {policy.notes || '-'}
                            </td>
                            <td className="p-4">
                              <div className="flex justify-end gap-2">
                                {isManaged ? (
                                  <Tooltip content={`This policy is managed by policy pack ${policy.managed_pack || 'unknown'}${policy.managed_version ? ` (v${policy.managed_version})` : ''}. Edit the YAML file and re-apply the pack to modify it.`}>
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      disabled
                                    >
                                      <Edit className="h-4 w-4" />
                                    </Button>
                                  </Tooltip>
                                ) : (
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={() => handleEdit(policy)}
                                  >
                                    <Edit className="h-4 w-4" />
                                  </Button>
                                )}
                                {isManaged ? (
                                  <Tooltip content={`This policy is managed by policy pack ${policy.managed_pack || 'unknown'}${policy.managed_version ? ` (v${policy.managed_version})` : ''}. Edit the YAML file and re-apply the pack to delete it.`}>
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      disabled
                                    >
                                      <Trash2 className="h-4 w-4" />
                                    </Button>
                                  </Tooltip>
                                ) : (
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={() => handleDelete(policy.id)}
                                  >
                                    <Trash2 className="h-4 w-4" />
                                  </Button>
                                )}
                              </div>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>

                {/* Mobile Cards */}
                <div className="block md:hidden space-y-3 p-4">
                  {paginatedPolicies.map((policy) => {
                    const isManaged = policy.managed === true
                    return (
                      <TableCard key={policy.id}>
                        <TableCardRow
                          label="Host"
                          value={<span className="font-mono text-sm">{policy.host}</span>}
                        />
                        <TableCardRow
                          label="Managed"
                          value={
                            isManaged ? (
                              <div className="flex flex-col gap-1">
                                <Badge variant="default">Managed</Badge>
                                {policy.managed_pack && (
                                  <span className="text-xs text-muted-foreground">
                                    {policy.managed_pack}
                                    {policy.managed_version && ` v${policy.managed_version}`}
                                  </span>
                                )}
                              </div>
                            ) : (
                              <span className="text-muted-foreground text-sm">-</span>
                            )
                          }
                        />
                        <TableCardRow
                          label="Killswitch"
                          value={
                            policy.killswitch_required ? (
                              <Tooltip content="Killswitch service is required for this host. Requests must pass through Killswitch before proceeding.">
                                <Badge variant="success">Required</Badge>
                              </Tooltip>
                            ) : (
                              <Tooltip content="Killswitch service is not required for this host. Requests can proceed without Killswitch checks.">
                                <Badge variant="danger">－</Badge>
                              </Tooltip>
                            )
                          }
                        />
                        <TableCardRow
                          label="Gatekeeper"
                          value={
                            policy.gatekeeper_required ? (
                              <Tooltip content="Gatekeeper service is required for this host. Requests must pass through Gatekeeper authorization before proceeding.">
                                <Badge variant="success">Required</Badge>
                              </Tooltip>
                            ) : (
                              <Tooltip content="Gatekeeper service is not required for this host. Requests can proceed without Gatekeeper authorization checks.">
                                <Badge variant="danger">－</Badge>
                              </Tooltip>
                            )
                          }
                        />
                        <TableCardRow
                          label="Notes"
                          value={
                            <span className="text-sm text-muted-foreground">
                              {policy.notes || '-'}
                            </span>
                          }
                        />
                        <TableCardActions>
                          {isManaged ? (
                            <>
                              <Tooltip content={`This policy is managed by policy pack ${policy.managed_pack || 'unknown'}${policy.managed_version ? ` (v${policy.managed_version})` : ''}. Edit the YAML file and re-apply the pack to modify it.`}>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  disabled
                                  className="flex-1"
                                >
                                  <Edit className="h-4 w-4 mr-2" />
                                  Edit
                                </Button>
                              </Tooltip>
                              <Tooltip content={`This policy is managed by policy pack ${policy.managed_pack || 'unknown'}${policy.managed_version ? ` (v${policy.managed_version})` : ''}. Edit the YAML file and re-apply the pack to delete it.`}>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  disabled
                                  className="flex-1"
                                >
                                  <Trash2 className="h-4 w-4 mr-2" />
                                  Delete
                                </Button>
                              </Tooltip>
                            </>
                          ) : (
                            <>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleEdit(policy)}
                                className="flex-1"
                              >
                                <Edit className="h-4 w-4 mr-2" />
                                Edit
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleDelete(policy.id)}
                                className="flex-1"
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                Delete
                              </Button>
                            </>
                          )}
                        </TableCardActions>
                      </TableCard>
                    )
                  })}
                </div>
                {/* Pagination Controls */}
                {totalPages > 1 && (
                  <div className="flex flex-col sm:flex-row items-center justify-between gap-3 border-t px-2 sm:px-4 py-3 bg-card">
                    <div className="text-xs sm:text-sm text-muted-foreground">
                      <span className="hidden sm:inline">Showing {startIndex + 1} to {Math.min(endIndex, filteredPolicies.length)} of {filteredPolicies.length} policies</span>
                      <span className="sm:hidden">{currentPage} / {totalPages}</span>
                    </div>
                    <div className="flex items-center gap-1 sm:gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setCurrentPage(prev => Math.max(1, prev - 1))}
                        disabled={currentPage === 1}
                        className="h-8"
                      >
                        <ChevronLeft className="h-4 w-4 sm:mr-1" />
                        <span className="hidden sm:inline">Previous</span>
                      </Button>
                      <div className="flex items-center gap-1">
                        {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                          let pageNum: number
                          if (totalPages <= 5) {
                            pageNum = i + 1
                          } else if (currentPage <= 3) {
                            pageNum = i + 1
                          } else if (currentPage >= totalPages - 2) {
                            pageNum = totalPages - 4 + i
                          } else {
                            pageNum = currentPage - 2 + i
                          }
                          return (
                            <Button
                              key={pageNum}
                              variant={currentPage === pageNum ? 'default' : 'outline'}
                              size="sm"
                              onClick={() => setCurrentPage(pageNum)}
                              className="min-w-[2rem] sm:min-w-[2.5rem] h-8"
                            >
                              {pageNum}
                            </Button>
                          )
                        })}
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setCurrentPage(prev => Math.min(totalPages, prev + 1))}
                        disabled={currentPage === totalPages}
                        className="h-8"
                      >
                        <span className="hidden sm:inline">Next</span>
                        <ChevronRight className="h-4 w-4 sm:ml-1" />
                      </Button>
                    </div>
                  </div>
                )}
              </>
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
  )
}
