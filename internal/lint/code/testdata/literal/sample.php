<?php
namespace App;

require_once 'bootstrap/autoload.php';

#[Flow\Scope('singleton')]
final class Importer
{
    #[ORM\Column(type: 'string')]
    private string $label = 'Import the orders';

    public function run(string $path, object $order): void
    {
        // Reads the queue once.
        $headers = ['Content-Type' => 'Write the header'];
        $type = $headers['Accept'] ?? ['Read the queue first'];
        throw new \RuntimeException(sprintf('Could not read %s', $path));
        echo "Read $path for {$order->id} now\n";
        $help = <<<EOT
            Usage: import $path
              Then check the log.
            EOT;
        $raw = <<<'EOT'
            Nowdoc keeps $path as written.
            EOT;
        $quote = 'It\'s done';
        $cite = <<<EOT
            'Quoted' opens and closes 'this'
            EOT;
    }
}
