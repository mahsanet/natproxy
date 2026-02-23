enum LogLevel { info, warn, error, success }

class LogEntry {
  final String time;
  final LogLevel level;
  final String message;

  const LogEntry({
    required this.time,
    required this.level,
    required this.message,
  });

  factory LogEntry.fromJson(Map<String, dynamic> json) {
    return LogEntry(
      time: json['t'] as String? ?? '',
      level: _parseLevel(json['l'] as String? ?? 'info'),
      message: json['m'] as String? ?? '',
    );
  }

  static LogLevel _parseLevel(String l) {
    switch (l) {
      case 'warn':
        return LogLevel.warn;
      case 'error':
        return LogLevel.error;
      case 'success':
        return LogLevel.success;
      default:
        return LogLevel.info;
    }
  }
}
