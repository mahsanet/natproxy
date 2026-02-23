class ServerListing {
  final String id;
  final String name;
  final String room;
  final String code;
  final String method;
  final String transport;
  final String protocol;

  const ServerListing({
    required this.id,
    required this.name,
    this.room = '',
    required this.code,
    this.method = '',
    this.transport = '',
    this.protocol = '',
  });

  factory ServerListing.fromJson(Map<String, dynamic> json) {
    return ServerListing(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      room: json['room'] as String? ?? '',
      code: json['code'] as String? ?? '',
      method: json['method'] as String? ?? '',
      transport: json['transport'] as String? ?? '',
      protocol: json['protocol'] as String? ?? '',
    );
  }
}
