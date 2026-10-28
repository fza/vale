<?php
require_once 'ZQX.php';

// Holds a ZQX for the run.
#[Flow\Scope('ZQX')]
final class Handler
{
    public function run(string $ZQX): void
    {
        $map = ['ZQX' => 'Write the ZQX report'];
        echo "Read {$ZQX} and $ZQX, then the ZQX log";
        $help = <<<EOT
            A ZQX heading.
              Then a ZQX line.
            EOT;
        $raw = <<<'EOT'
            Keeps $ZQX as written.
            EOT;
    }
}
