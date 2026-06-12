'use client'

import { useEffect, useState, useRef, useCallback } from 'react'
import { AppLayout } from '@/components/layout/app-layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Activity, Play, Square, Loader2, Wifi, WifiOff,
  Send, Trash2, Zap, Clock, Radio, MessageSquare,
  Users, Plug
} from 'lucide-react'

interface RTMessage {
  client_id?: string
  event: string
  channel?: string
  topic?: string
  data?: any
  timestamp: number
}

export default function RealtimePage() {
  const [connected, setConnected] = useState(false)
  const [transport, setTransport] = useState<'sse' | 'ws' | null>(null)
  const [clientId, setClientId] = useState<string | null>(null)
  const [messages, setMessages] = useState<RTMessage[]>([])
  const [subscriptions, setSubscriptions] = useState<string[]>([])
  const [newTopic, setNewTopic] = useState('')
  const [broadcastMsg, setBroadcastMsg] = useState('')
  const [broadcastTopic, setBroadcastTopic] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [stats, setStats] = useState({ sent: 0, received: 0, errors: 0 })

  const eventSourceRef = useRef<EventSource | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => { scrollToBottom() }, [messages])

  const addMessage = useCallback((msg: RTMessage) => {
    setMessages(prev => [...prev.slice(-200), msg])
    setStats(s => ({ ...s, received: s.received + 1 }))
  }, [])

  const connectSSE = useCallback(() => {
    if (eventSourceRef.current) return

    const baseUrl = window.location.origin
    const es = new EventSource(`${baseUrl}/api/v1/sse`)
    eventSourceRef.current = es

    es.onopen = () => {
      setConnected(true)
      setTransport('sse')
      setError(null)
      addMessage({ event: 'connection:established', timestamp: Date.now() })
    }

    es.onmessage = (event) => {
      try {
        const msg: RTMessage = JSON.parse(event.data)
        if (msg.event === 'connection:established' && msg.client_id) {
          setClientId(msg.client_id)
        }
        addMessage(msg)
      } catch {}
    }

    es.onerror = () => {
      setConnected(false)
      setTransport(null)
      setError('SSE connection lost. Retrying...')
      setStats(s => ({ ...s, errors: s.errors + 1 }))
    }
  }, [addMessage])

  const connectWS = useCallback(() => {
    if (wsRef.current) return

    const token = localStorage.getItem('gresbase_token')
    const wsUrl = window.location.origin.replace(/^http/, 'ws')
    const url = `${wsUrl}/api/v1/realtime${token ? `?token=${token}` : ''}`
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setConnected(true)
      setTransport('ws')
      setError(null)
      addMessage({ event: 'connection:established', timestamp: Date.now() })
      // Resubscribe
      for (const topic of subscriptions) {
        ws.send(JSON.stringify({ type: 'subscribe', subscriptions: [topic] }))
      }
    }

    ws.onmessage = (event) => {
      try {
        const msg: RTMessage = JSON.parse(event.data)
        if (msg.event === 'connection:established' && msg.client_id) {
          setClientId(msg.client_id)
        }
        addMessage(msg)
      } catch {}
    }

    ws.onclose = () => {
      setConnected(false)
      setTransport(null)
      wsRef.current = null
    }

    ws.onerror = () => {
      setError('WebSocket error')
      setStats(s => ({ ...s, errors: s.errors + 1 }))
    }
  }, [addMessage, subscriptions])

  const disconnect = useCallback(() => {
    eventSourceRef.current?.close()
    eventSourceRef.current = null
    wsRef.current?.close()
    wsRef.current = null
    setConnected(false)
    setTransport(null)
    setClientId(null)
  }, [])

  const subscribe = useCallback((topic: string) => {
    if (!topic || subscriptions.includes(topic)) return
    setSubscriptions(prev => [...prev, topic])

    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'subscribe', subscriptions: [topic] }))
    } else if (clientId) {
      fetch('/api/v1/realtime', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'subscribe', clientId, subscriptions: [topic] }),
      }).catch(() => {})
    }

    addMessage({ event: 'subscription:confirmed', topic, timestamp: Date.now() })
  }, [subscriptions, clientId, addMessage])

  const unsubscribe = useCallback((topic: string) => {
    setSubscriptions(prev => prev.filter(t => t !== topic))

    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'unsubscribe', subscriptions: [topic] }))
    } else if (clientId) {
      fetch('/api/v1/realtime', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'unsubscribe', clientId, subscriptions: [topic] }),
      }).catch(() => {})
    }
  }, [clientId])

  const sendMessage = useCallback(() => {
    if (!broadcastMsg || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return
    const topic = broadcastTopic || 'test'
    wsRef.current.send(JSON.stringify({
      type: 'message',
      event: 'user:message',
      topic,
      data: { text: broadcastMsg, timestamp: Date.now() },
    }))
    setBroadcastMsg('')
    setStats(s => ({ ...s, sent: s.sent + 1 }))
    addMessage({ event: 'user:message', topic, data: { text: broadcastMsg }, timestamp: Date.now(), client_id: clientId || 'me' })
  }, [broadcastMsg, broadcastTopic, clientId, addMessage])

  const clearMessages = () => setMessages([])

  return (
    <AppLayout>
      <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Realtime</h1>
          <p className="text-sm text-muted-foreground mt-1">
            SSE + WebSocket realtime engine — test subscriptions and broadcasting
          </p>
        </div>
        <div className="flex items-center gap-2">
          {connected ? (
            <Button variant="destructive" size="sm" onClick={disconnect}>
              <Square className="mr-2 h-4 w-4" /> Disconnect
            </Button>
          ) : (
            <>
              <Button size="sm" onClick={connectSSE}>
                <Radio className="mr-2 h-4 w-4" /> SSE Connect
              </Button>
              <Button size="sm" variant="outline" onClick={connectWS}>
                <Plug className="mr-2 h-4 w-4" /> WS Connect
              </Button>
            </>
          )}
        </div>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3">
            <p className="text-sm text-red-500">{error}</p>
          </CardContent>
        </Card>
      )}

      {/* Connection Status */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            {connected ? (
              <Wifi className="h-5 w-5 text-emerald-500" />
            ) : (
              <WifiOff className="h-5 w-5 text-muted-foreground" />
            )}
            <div>
              <div className="text-sm font-medium">{connected ? 'Connected' : 'Disconnected'}</div>
              <div className="text-xs text-muted-foreground">{transport?.toUpperCase() || '—'}</div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <MessageSquare className="h-5 w-5 text-blue-500" />
            <div>
              <div className="text-sm font-medium">{stats.received}</div>
              <div className="text-xs text-muted-foreground">Received</div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <Send className="h-5 w-5 text-amber-500" />
            <div>
              <div className="text-sm font-medium">{stats.sent}</div>
              <div className="text-xs text-muted-foreground">Sent</div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <Zap className="h-5 w-5 text-purple-500" />
            <div>
              <div className="text-sm font-medium">{subscriptions.length}</div>
              <div className="text-xs text-muted-foreground">Subscriptions</div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Client Info */}
      {clientId && (
        <Card>
          <CardContent className="flex items-center gap-3 py-3">
            <Users className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">Client ID:</span>
            <code className="text-xs bg-muted px-2 py-1 rounded font-mono">{clientId}</code>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Subscriptions Panel */}
        <Card className="lg:col-span-1">
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Activity className="h-4 w-4" />
              Subscriptions
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex gap-2">
              <Input
                placeholder="Topic (e.g. posts/*)"
                value={newTopic}
                onChange={e => setNewTopic(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && (subscribe(newTopic), setNewTopic(''))}
                className="text-sm"
              />
              <Button
                size="sm"
                onClick={() => { subscribe(newTopic); setNewTopic('') }}
                disabled={!newTopic || !connected}
              >
                Add
              </Button>
            </div>

            {subscriptions.length === 0 && (
              <p className="text-xs text-muted-foreground text-center py-4">
                No active subscriptions
              </p>
            )}

            {subscriptions.map(topic => (
              <div key={topic} className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
                  </span>
                  <code className="text-xs font-mono">{topic}</code>
                </div>
                <Button variant="ghost" size="icon" onClick={() => unsubscribe(topic)}>
                  <Trash2 className="h-3 w-3 text-muted-foreground" />
                </Button>
              </div>
            ))}

            {/* Broadcast (WS only) */}
            {transport === 'ws' && (
              <div className="border-t border-border pt-3 space-y-2">
                <label className="text-xs font-medium text-muted-foreground">Broadcast Message</label>
                <div className="flex gap-2">
                  <Input
                    placeholder="Topic"
                    value={broadcastTopic}
                    onChange={e => setBroadcastTopic(e.target.value)}
                    className="text-sm w-1/3"
                  />
                  <Input
                    placeholder="Message text"
                    value={broadcastMsg}
                    onChange={e => setBroadcastMsg(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && sendMessage()}
                    className="text-sm flex-1"
                  />
                </div>
                <Button size="sm" className="w-full" onClick={sendMessage} disabled={!broadcastMsg}>
                  <Send className="mr-2 h-3 w-3" /> Broadcast
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Messages Panel */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Clock className="h-4 w-4" />
              Messages ({messages.length})
            </CardTitle>
            <Button variant="ghost" size="sm" onClick={clearMessages}>
              <Trash2 className="mr-2 h-3 w-3" /> Clear
            </Button>
          </CardHeader>
          <CardContent>
            <div className="h-[400px] overflow-y-auto space-y-1 rounded-lg bg-muted/20 p-2 font-mono text-xs">
              {messages.length === 0 && (
                <div className="flex items-center justify-center h-full text-muted-foreground">
                  <p>Messages will appear here</p>
                </div>
              )}
              {messages.map((msg, i) => (
                <div
                  key={i}
                  className={`flex items-start gap-2 rounded px-2 py-1 ${
                    msg.event === 'connection:established' ? 'bg-emerald-500/10 text-emerald-600' :
                    msg.event?.startsWith('subscription:') ? 'bg-blue-500/10 text-blue-600' :
                    msg.event?.startsWith('presence:') ? 'bg-purple-500/10 text-purple-600' :
                    msg.event === 'user:message' ? 'bg-amber-500/10 text-amber-600' :
                    'bg-muted/50'
                  }`}
                >
                  <span className="text-muted-foreground shrink-0 w-16">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                  <Badge variant="outline" className="text-[10px] shrink-0">
                    {msg.event}
                  </Badge>
                  {msg.topic && (
                    <span className="text-blue-400 shrink-0">{msg.topic}</span>
                  )}
                  {msg.data && (
                    <span className="truncate text-muted-foreground">
                      {typeof msg.data === 'string' ? msg.data : JSON.stringify(msg.data).slice(0, 120)}
                    </span>
                  )}
                </div>
              ))}
              <div ref={messagesEndRef} />
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
    </AppLayout>
  )
}
