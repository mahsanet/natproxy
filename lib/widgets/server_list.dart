import 'package:flutter/material.dart';
import '../models/server_listing.dart';

class ServerListCard extends StatelessWidget {
  final List<ServerListing> servers;
  final bool isLoading;
  final String? error;
  final bool isLive;
  final VoidCallback onRefresh;
  final ValueChanged<ServerListing> onServerTap;

  const ServerListCard({
    super.key,
    required this.servers,
    required this.isLoading,
    this.error,
    this.isLive = false,
    required this.onRefresh,
    required this.onServerTap,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.dns, size: 20),
                const SizedBox(width: 8),
                Text(
                  'Available Servers',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                if (isLive) ...[
                  const SizedBox(width: 6),
                  Container(
                    width: 8,
                    height: 8,
                    decoration: const BoxDecoration(
                      color: Colors.green,
                      shape: BoxShape.circle,
                    ),
                  ),
                ],
                const Spacer(),
                IconButton(
                  icon: const Icon(Icons.refresh),
                  onPressed: isLoading ? null : onRefresh,
                  iconSize: 20,
                ),
              ],
            ),
            const SizedBox(height: 8),
            if (isLoading)
              const Center(
                child: Padding(
                  padding: EdgeInsets.all(16),
                  child: CircularProgressIndicator(),
                ),
              )
            else if (error != null)
              Column(
                children: [
                  Text(
                    error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                  const SizedBox(height: 8),
                  TextButton(onPressed: onRefresh, child: const Text('Retry')),
                ],
              )
            else if (servers.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: Center(
                  child: Text(
                    'No servers found',
                    style: TextStyle(color: Colors.grey),
                  ),
                ),
              )
            else
              ...servers.map(
                (server) => ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.cloud),
                  title: Text(server.name),
                  subtitle: Text(
                    [
                      if (server.protocol.isNotEmpty)
                        server.protocol.toUpperCase(),
                      if (server.transport.isNotEmpty) server.transport,
                      if (server.method.isNotEmpty) server.method,
                    ].join(' / '),
                  ),
                  trailing: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (server.room.isNotEmpty)
                        Padding(
                          padding: const EdgeInsets.only(right: 8),
                          child: Chip(
                            label: Text(
                              server.room,
                              style: const TextStyle(fontSize: 11),
                            ),
                            padding: EdgeInsets.zero,
                            materialTapTargetSize:
                                MaterialTapTargetSize.shrinkWrap,
                          ),
                        ),
                      const Icon(Icons.chevron_right),
                    ],
                  ),
                  onTap: () => onServerTap(server),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
