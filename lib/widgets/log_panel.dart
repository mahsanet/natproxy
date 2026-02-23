import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../models/log_entry.dart';

class LogPanel extends StatefulWidget {
  final List<LogEntry> entries;
  final VoidCallback onClear;

  const LogPanel({super.key, required this.entries, required this.onClear});

  @override
  State<LogPanel> createState() => _LogPanelState();
}

class _LogPanelState extends State<LogPanel> {
  bool _expanded = false;
  final _scrollController = ScrollController();
  bool _autoScroll = true;
  int _lastSeenCount = 0;

  @override
  void didUpdateWidget(LogPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.entries.length > oldWidget.entries.length &&
        _autoScroll &&
        _expanded) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _scrollToBottom());
    }
  }

  void _scrollToBottom() {
    if (_scrollController.hasClients) {
      _scrollController.jumpTo(_scrollController.position.maxScrollExtent);
    }
  }

  int get _unreadCount {
    if (_expanded) {
      _lastSeenCount = widget.entries.length;
      return 0;
    }
    return widget.entries.length - _lastSeenCount;
  }

  Color _levelColor(LogLevel level) {
    switch (level) {
      case LogLevel.info:
        return Colors.blue.shade300;
      case LogLevel.warn:
        return Colors.amber.shade300;
      case LogLevel.error:
        return Colors.red.shade300;
      case LogLevel.success:
        return Colors.green.shade300;
    }
  }

  String _levelTag(LogLevel level) {
    switch (level) {
      case LogLevel.info:
        return 'INF';
      case LogLevel.warn:
        return 'WRN';
      case LogLevel.error:
        return 'ERR';
      case LogLevel.success:
        return 'OK ';
    }
  }

  void _copyAll() {
    final text = widget.entries
        .map((e) => '${e.time} [${_levelTag(e.level).trim()}] ${e.message}')
        .join('\n');
    Clipboard.setData(ClipboardData(text: text));
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Logs copied to clipboard'),
          duration: Duration(seconds: 2),
        ),
      );
    }
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final unread = _unreadCount;
    return AnimatedContainer(
      duration: const Duration(milliseconds: 250),
      curve: Curves.easeInOut,
      height: _expanded ? 280 : 48,
      decoration: const BoxDecoration(
        color: Color(0xFF1E1E2E),
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      child: Column(
        children: [
          // Header
          GestureDetector(
            onTap: () => setState(() {
              _expanded = !_expanded;
              if (_expanded) {
                _lastSeenCount = widget.entries.length;
                WidgetsBinding.instance.addPostFrameCallback(
                  (_) => _scrollToBottom(),
                );
              }
            }),
            child: Container(
              height: 48,
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Row(
                children: [
                  const Icon(Icons.terminal, size: 18, color: Colors.white70),
                  const SizedBox(width: 8),
                  const Text(
                    'Logs',
                    style: TextStyle(
                      color: Colors.white70,
                      fontSize: 14,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  if (unread > 0) ...[
                    const SizedBox(width: 8),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.blue.shade300,
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        '$unread',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 11,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ),
                  ],
                  const Spacer(),
                  if (_expanded) ...[
                    _headerButton(Icons.copy, _copyAll),
                    const SizedBox(width: 4),
                    _headerButton(Icons.delete_outline, widget.onClear),
                    const SizedBox(width: 4),
                  ],
                  Icon(
                    _expanded ? Icons.expand_more : Icons.expand_less,
                    size: 20,
                    color: Colors.white70,
                  ),
                ],
              ),
            ),
          ),
          // Body
          if (_expanded)
            Expanded(
              child: NotificationListener<ScrollNotification>(
                onNotification: (notification) {
                  if (notification is ScrollUpdateNotification) {
                    final pos = _scrollController.position;
                    _autoScroll = pos.pixels >= pos.maxScrollExtent - 20;
                  }
                  return false;
                },
                child: ListView.builder(
                  controller: _scrollController,
                  padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
                  itemCount: widget.entries.length,
                  itemExtent: 20,
                  itemBuilder: (context, index) {
                    final e = widget.entries[index];
                    return Row(
                      children: [
                        Text(
                          e.time,
                          style: const TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 11,
                            color: Colors.white38,
                          ),
                        ),
                        const SizedBox(width: 6),
                        Text(
                          _levelTag(e.level),
                          style: TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 11,
                            color: _levelColor(e.level),
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                        const SizedBox(width: 6),
                        Expanded(
                          child: Text(
                            e.message,
                            style: const TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11,
                              color: Colors.white70,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    );
                  },
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _headerButton(IconData icon, VoidCallback onTap) {
    return GestureDetector(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.all(4),
        child: Icon(icon, size: 18, color: Colors.white54),
      ),
    );
  }
}
